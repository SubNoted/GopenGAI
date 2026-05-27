package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gopengai/internal/db"
)

// ---------------------------------------------------------------------------
// Context keys for delegate tool
// ---------------------------------------------------------------------------

const (
	// ContextKeyAgentRegistry is the context key for the agent registry (used
	// by the delegate tool to look up sub-agents).
	ContextKeyAgentRegistry contextKey = "agent_registry"

	// ContextKeyLLMClient is the context key for the LLM client (used by the
	// delegate tool to call the sub-agent's LLM).
	ContextKeyLLMClient contextKey = "llm_client"

	// ContextKeyVisitedAgents is the context key for cycle detection.
	// The value is a map[string]bool of agent names already in the delegation
	// chain.
	ContextKeyVisitedAgents contextKey = "visited_agents"

	// ContextKeyCurrentMessageID is the context key for the current parent
	// message ID (used for delegation logging).
	ContextKeyCurrentMessageID contextKey = "current_message_id"
)

// ---------------------------------------------------------------------------
// AgentRegistry interface (avoids import cycle with internal/agent)
// ---------------------------------------------------------------------------

// AgentLite is a minimal agent interface needed by the delegate tool.
// It's extracted to avoid a circular import with the agent package.
type AgentLite interface {
	Name() string
	SystemPrompt() string
	Model() string
	HasTool(name string) bool
	IsToolAllowed(name string) bool
}

// AgentRegistryLite is the subset of the agent registry that the delegate
// tool needs.
type AgentRegistryLite interface {
	Get(name string) (AgentLite, error)
}

// ---------------------------------------------------------------------------
// LLMClientLite interface (avoids import cycle with internal/llm)
// ---------------------------------------------------------------------------

// LLMClientLite is the subset of the LLM client that the delegate tool needs.
type LLMClientLite interface {
	ChatCompletion(ctx context.Context, messages []LLMMessage) (string, error)
}

// LLMMessage is a simplified message type for the delegate LLM call.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ---------------------------------------------------------------------------
// DelegateTool
// ---------------------------------------------------------------------------

// DelegateTool delegates a task to a sub-agent. It loads the sub-agent from
// the registry, builds a context with the sub-agent's system prompt and the
// given task, calls the LLM, and returns the result.
//
// Cycle detection: if an agent name is already in the visited set (passed via
// context), the delegation is rejected with an error.
//
// Timeout: the sub-agent LLM call has a 30-second timeout derived from the
// context.
type DelegateTool struct{}

// Name returns the tool name.
func (d *DelegateTool) Name() string { return "delegate" }

// Description returns a human-readable description.
func (d *DelegateTool) Description() string {
	return "Delegate a task to another agent. The sub-agent will be loaded from the agent registry " +
		"and will process the task using its own system prompt and tools. Use this for tasks that " +
		"require a different expertise or perspective."
}

// Parameters returns the JSON Schema for the tool's arguments.
func (d *DelegateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent_name": {
				"type": "string",
				"description": "The name of the agent to delegate to (must be registered in the agent registry)"
			},
			"task": {
				"type": "string",
				"description": "A clear description of the task to be performed by the sub-agent"
			}
		},
		"required": ["agent_name", "task"]
	}`)
}

// Execute delegates a task to a sub-agent.
func (d *DelegateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		AgentName string `json:"agent_name"`
		Task      string `json:"task"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("delegate: invalid arguments: %w", err)
	}
	if params.AgentName == "" {
		return "", fmt.Errorf("delegate: agent_name is required")
	}
	if params.Task == "" {
		return "", fmt.Errorf("delegate: task is required")
	}

	// --- Cycle detection ---
	visited, _ := ctx.Value(ContextKeyVisitedAgents).(map[string]bool)
	if visited == nil {
		visited = make(map[string]bool)
	}
	currentAgent := AgentNameFromContext(ctx)
	if visited[params.AgentName] {
		return "", fmt.Errorf("delegate: cycle detected — agent %q is already in the delegation chain (called from %q)", params.AgentName, currentAgent)
	}

	// --- Load sub-agent ---
	agentReg, _ := ctx.Value(ContextKeyAgentRegistry).(AgentRegistryLite)
	if agentReg == nil {
		return "", fmt.Errorf("delegate: agent registry not set in context")
	}
	subAgent, err := agentReg.Get(params.AgentName)
	if err != nil {
		return "", fmt.Errorf("delegate: agent %q not found: %w", params.AgentName, err)
	}

	// --- Get LLM client ---
	llmClient, _ := ctx.Value(ContextKeyLLMClient).(LLMClientLite)
	if llmClient == nil {
		return "", fmt.Errorf("delegate: LLM client not set in context")
	}

	// --- Mark current delegation and propagate visited set ---
	visited[params.AgentName] = true
	ctx = context.WithValue(ctx, ContextKeyVisitedAgents, visited)

	// --- Add timeout protection (30s max for sub-agent) ---
	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// --- Build messages for sub-agent ---
	messages := []LLMMessage{
		{Role: "system", Content: subAgent.SystemPrompt()},
		{Role: "user", Content: params.Task},
	}

	// --- Call LLM ---
	startTime := time.Now()
	result, err := llmClient.ChatCompletion(subCtx, messages)
	durationMs := time.Since(startTime).Milliseconds()
	if err != nil {
		// Log the failed delegation.
		d.logDelegation(ctx, params.AgentName, params.Task, durationMs, err.Error())
		return "", fmt.Errorf("delegate: sub-agent %q failed: %w", params.AgentName, err)
	}

	// --- Log successful delegation ---
	d.logDelegation(ctx, params.AgentName, params.Task, durationMs, "")

	return fmt.Sprintf("Delegated to %q. Result:\n%s", params.AgentName, result), nil
}

// logDelegation writes a delegation log entry if a querier and message ID are
// available in the context.
func (d *DelegateTool) logDelegation(ctx context.Context, childAgent, task string, durationMs int64, errMsg string) {
	q := QuerierFromContext(ctx)
	if q == nil {
		return
	}
	msgID, _ := ctx.Value(ContextKeyCurrentMessageID).(string)
	if msgID == "" {
		return
	}

	var summary string
	if errMsg != "" {
		summary = "error: " + errMsg
	} else {
		summary = "completed successfully"
	}

	_, logErr := q.CreateDelegationLog(ctx, db.CreateDelegationLogParams{
		ID:              newToolID(),
		ParentMessageID: msgID,
		ChildAgentName:  childAgent,
		TaskDescription: task,
		ResultSummary:   sql.NullString{String: summary, Valid: summary != ""},
		DurationMs:      sql.NullInt64{Int64: durationMs, Valid: true},
		CreatedAt:       time.Now().UnixMilli(),
	})
	if logErr != nil {
		// Non-fatal: just log the failure to create the delegation log.
		fmt.Printf("delegate: failed to log delegation: %v\n", logErr)
	}
}
