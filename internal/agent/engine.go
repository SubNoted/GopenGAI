package agent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/llm"
	"gopengai/internal/tools"
)

// ---------------------------------------------------------------------------
// EventBus interface (implemented by api.EventBus, defined here to avoid
// circular import between internal/agent and internal/api)
// ---------------------------------------------------------------------------

// EventBus defines the interface for publishing events during engine execution.
// The concrete implementation lives in internal/api/events.go.
// Uses primitive types in the interface to avoid type coupling.
type EventBus interface {
	PublishGlobal(eventType string, properties interface{})
	PublishSession(sessionID string, eventType string, properties interface{})
}

// ---------------------------------------------------------------------------
// HistoryRepository interface (implemented by history.Repository, defined
// here to avoid circular import — history imports agent, so agent can't
// import history)
// ---------------------------------------------------------------------------

// HistoryRepository defines the subset of history.Repository methods needed
// by the engine. The concrete implementation lives in internal/history.
type HistoryRepository interface {
	InsertMessage(ctx context.Context, params db.CreateMessageParams) (db.Message, error)
	GetSession(ctx context.Context, id string) (db.Session, error)
	BuildContext(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error)
	UpdateActiveLeaf(ctx context.Context, sessionID, leafID string) error
}

// ---------------------------------------------------------------------------
// Engine — core agent processing loop
// ---------------------------------------------------------------------------

// Engine runs the core agent loop: load agent, build context, call LLM,
// execute tools, persist messages, publish events. It is designed to be
// called asynchronously (goroutine) and supports abort via context cancellation.
type Engine struct {
	LLM      *llm.Client
	Tools    *tools.Registry
	History  HistoryRepository
	Agents   *Registry
	SQLDB    *sql.DB
	Querier  db.Querier
	Config   *config.Config
	EventBus EventBus

	abortMu    sync.RWMutex
	abortFuncs map[string]context.CancelFunc // sessionID → cancel
	idCounter  atomic.Int64                  // monotonic counter for fallback IDs
}

// NewEngine creates an Engine with all required dependencies.
func NewEngine(
	llmClient *llm.Client,
	toolReg *tools.Registry,
	histRepo HistoryRepository,
	agentReg *Registry,
	sqldb *sql.DB,
	querier db.Querier,
	cfg *config.Config,
	eventBus EventBus,
) *Engine {
	return &Engine{
		LLM:        llmClient,
		Tools:      toolReg,
		History:    histRepo,
		Agents:     agentReg,
		SQLDB:      sqldb,
		Querier:    querier,
		Config:     cfg,
		EventBus:   eventBus,
		abortFuncs: make(map[string]context.CancelFunc),
	}
}

// ---------------------------------------------------------------------------
// Process — main entry point (async)
// ---------------------------------------------------------------------------

// Process handles a user message within a session asynchronously. It:
//  1. Saves the user message to the conversation tree
//  2. Builds LLM context from the active branch
//  3. Calls the LLM with tool definitions
//  4. If LLM returns tool_calls: executes tools, saves results, repeats
//  5. If LLM returns text: saves assistant response, updates active leaf
//
// Process publishes events via EventBus throughout execution:
//   - session.status (working / idle)
//   - message.llm.started
//   - message.part.added / message.part.updated
//   - message.tool.started / message.tool.completed / message.tool.error
//   - message.complete
//   - message.error
//
// Process is safe to call as a goroutine. It supports abort via Engine.Abort().
func (e *Engine) Process(ctx context.Context, sessionID, userContent, agentName string) error {
	// Validate input: empty content is not allowed.
	if userContent == "" {
		return fmt.Errorf("engine: empty user content")
	}

	// Create abort-capable derived context.
	ctx, cancel := context.WithCancel(ctx)
	e.setAbortFunc(sessionID, cancel)
	defer e.clearAbortFunc(sessionID)
	defer cancel()

	// -----------------------------------------------------------------------
	// Publish: session.status = working
	// -----------------------------------------------------------------------
	e.publishSessionEvent(sessionID, "session.status", map[string]string{
		"session_id": sessionID,
		"status":     "working",
	})
	defer func() {
		e.publishSessionEvent(sessionID, "session.status", map[string]string{
			"session_id": sessionID,
			"status":     "idle",
		})
	}()

	// -----------------------------------------------------------------------
	// Load agent
	// -----------------------------------------------------------------------
	agent, err := e.Agents.Get(agentName)
	if err != nil {
		return fmt.Errorf("engine: agent %q not found: %w", agentName, err)
	}

	model := agent.Model
	if model == "" {
		model = e.Config.LLM.Model
	}

	// -----------------------------------------------------------------------
	// Load session
	// -----------------------------------------------------------------------
	session, err := e.History.GetSession(ctx, sessionID)
	if err != nil {
		e.publishSessionEvent(sessionID, "message.error", map[string]string{
			"error": fmt.Sprintf("session %q not found", sessionID),
		})
		return fmt.Errorf("engine: session %q not found: %w", sessionID, err)
	}

	// -----------------------------------------------------------------------
	// Save user message (parent = current active leaf)
	// -----------------------------------------------------------------------
	now := time.Now().UnixMilli()
	userMsgID := e.newID()
	_, err = e.History.InsertMessage(ctx, db.CreateMessageParams{
		ID:        userMsgID,
		SessionID: sessionID,
		ParentID:  session.ActiveLeafID,
		Role:      "user",
		Content:   sql.NullString{String: userContent, Valid: userContent != ""},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		e.publishSessionEvent(sessionID, "message.error", map[string]string{
			"error": fmt.Sprintf("save user message: %v", err),
		})
		return fmt.Errorf("engine: save user message: %w", err)
	}

	// -----------------------------------------------------------------------
	// Build LLM context: [system prompt + active branch]
	// -----------------------------------------------------------------------
	llmMessages, err := e.History.BuildContext(ctx, sessionID, agent.SystemPrompt, 0)
	if err != nil {
		e.publishSessionEvent(sessionID, "message.error", map[string]string{
			"error": fmt.Sprintf("build context: %v", err),
		})
		return fmt.Errorf("engine: build context: %w", err)
	}

	// Append the new user message (it was saved but active_leaf hasn't been
	// updated yet, so it's not included in BuildContext's branch).
	llmMessages = append(llmMessages, llm.Message{
		Role:    "user",
		Content: userContent,
	})

	// -----------------------------------------------------------------------
	// Determine tool definitions for this agent
	// -----------------------------------------------------------------------
	toolDefs := e.getToolDefinitions(agent)

	maxIterations := e.Config.LLM.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	// Track the parent message ID for the tree chain.
	// Initially the user message; each assistant message becomes the new parent.
	parentMsgID := userMsgID

	// -----------------------------------------------------------------------
	// Core LLM loop
	// -----------------------------------------------------------------------

	for iteration := 0; iteration < maxIterations; iteration++ {
		// Check for abort at loop boundaries.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("engine: aborted: %w", err)
		}

		e.publishSessionEvent(sessionID, "message.llm.started", map[string]interface{}{
			"iteration": iteration,
		})

		// Call LLM with current context + tool definitions.
		resp, err := e.LLM.ChatCompletion(ctx, &llm.ChatCompletionRequest{
			Model:    model,
			Messages: llmMessages,
			Tools:    toolDefs,
		})
		if err != nil {
			e.publishSessionEvent(sessionID, "message.error", map[string]string{
				"error": fmt.Sprintf("LLM call (iteration %d): %v", iteration, err),
			})
			return fmt.Errorf("engine: LLM call (iteration %d): %w", iteration, err)
		}

		if len(resp.Choices) == 0 {
			e.publishSessionEvent(sessionID, "message.error", map[string]string{
				"error": "LLM returned no choices",
			})
			return fmt.Errorf("engine: LLM returned no choices")
		}

		choice := resp.Choices[0]
		usage := resp.Usage

		// --------------------------------------------------------------------
		// Case 1: LLM produced a text response (finish_reason = "stop")
		// NOTE: Check ToolCalls first — some providers return finish_reason="stop"
		// alongside ToolCalls. If tool_calls are present, handle them even if
		// finish_reason is "stop".
		// --------------------------------------------------------------------
		if len(choice.Message.ToolCalls) == 0 && (choice.FinishReason == "stop" || choice.FinishReason == "") {
			content := choice.Message.Content
			now := time.Now().UnixMilli()

			assistantMsg, err := e.History.InsertMessage(ctx, db.CreateMessageParams{
				ID:         e.newID(),
				SessionID:  sessionID,
				ParentID:   sql.NullString{String: parentMsgID, Valid: true},
				Role:       "assistant",
				Content:    sql.NullString{String: content, Valid: content != ""},
				Model:      sql.NullString{String: model, Valid: model != ""},
				TokenCount: sql.NullInt64{Int64: int64(usage.TotalTokens), Valid: usage.TotalTokens > 0},
				CreatedAt:  now,
				UpdatedAt:  now,
			})
			if err != nil {
				e.publishSessionEvent(sessionID, "message.error", map[string]string{
					"error": fmt.Sprintf("save assistant message: %v", err),
				})
				return fmt.Errorf("engine: save assistant message: %w", err)
			}

			// Update active leaf BEFORE publishing completion — if the DB write
			// fails, the UI should not see a success event.
			if err := e.History.UpdateActiveLeaf(ctx, sessionID, assistantMsg.ID); err != nil {
				return fmt.Errorf("engine: update active leaf: %w", err)
			}

			// Publish streaming-style events.
			e.publishSessionEvent(sessionID, "message.part.added", map[string]interface{}{
				"message_id": assistantMsg.ID,
				"content":    content,
			})
			e.publishSessionEvent(sessionID, "message.complete", map[string]interface{}{
				"message_id": assistantMsg.ID,
				"content":    content,
				"role":       "assistant",
				"usage": map[string]int{
					"prompt_tokens":     usage.PromptTokens,
					"completion_tokens": usage.CompletionTokens,
					"total_tokens":      usage.TotalTokens,
				},
			})

			return nil
		}

		// --------------------------------------------------------------------
		// Case 2: LLM requested tool calls (finish_reason = "tool_calls")
		// --------------------------------------------------------------------
		if choice.FinishReason == "tool_calls" || len(choice.Message.ToolCalls) > 0 {
			toolCalls := choice.Message.ToolCalls

			// Serialize tool calls for database storage.
			toolArgsJSON, err := json.Marshal(toolCalls)
			if err != nil {
				e.publishSessionEvent(sessionID, "message.error", map[string]string{
					"error": fmt.Sprintf("marshal tool calls: %v", err),
				})
				return fmt.Errorf("engine: marshal tool calls: %w", err)
			}
			now := time.Now().UnixMilli()

			// Include Content if the LLM provided reasoning text alongside tool_calls.
			toolContent := choice.Message.Content

			assistantMsg, err := e.History.InsertMessage(ctx, db.CreateMessageParams{
				ID:         e.newID(),
				SessionID:  sessionID,
				ParentID:   sql.NullString{String: parentMsgID, Valid: true},
				Role:       "assistant",
				Content:    sql.NullString{String: toolContent, Valid: toolContent != ""},
				ToolArgs:   sql.NullString{String: string(toolArgsJSON), Valid: len(toolArgsJSON) > 0},
				Model:      sql.NullString{String: model, Valid: model != ""},
				TokenCount: sql.NullInt64{Int64: int64(usage.TotalTokens), Valid: usage.TotalTokens > 0},
				CreatedAt:  now,
				UpdatedAt:  now,
			})
			if err != nil {
				e.publishSessionEvent(sessionID, "message.error", map[string]string{
					"error": fmt.Sprintf("save tool call message: %v", err),
				})
				return fmt.Errorf("engine: save tool call message: %w", err)
			}

			// Add assistant message with tool_calls to in-memory context.
			llmMessages = append(llmMessages, llm.Message{
				Role:      "assistant",
				Content:   toolContent,
				ToolCalls: toolCalls,
			})

			// Set parent for the next iteration: tool results become children
			// of this assistant message.
			parentMsgID = assistantMsg.ID

			// Execute each tool call.
			for _, tc := range toolCalls {
				// Check for abort between tool calls.
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("engine: aborted during tool execution: %w", err)
				}

				toolName := tc.Function.Name

				e.publishSessionEvent(sessionID, "message.tool.started", map[string]interface{}{
					"tool_call_id": tc.ID,
					"tool_name":    toolName,
					"arguments":    tc.Function.Arguments,
				})

				// Check agent permission.
				if !agent.IsToolAllowed(toolName) {
					toolResult := fmt.Sprintf("Tool %q is not allowed for agent %q", toolName, agent.Name)
					e.publishSessionEvent(sessionID, "message.tool.error", map[string]interface{}{
						"tool_call_id": tc.ID,
						"tool_name":    toolName,
						"error":        toolResult,
					})
					e.saveToolResult(ctx, sessionID, assistantMsg.ID, toolName, tc.ID, toolResult)
					llmMessages = append(llmMessages, llm.Message{
						Role:       "tool",
						Content:    toolResult,
						ToolCallID: tc.ID,
						Name:       toolName,
					})
					continue
				}

				// Look up tool in registry.
				tool, err := e.Tools.Get(toolName)
				if err != nil {
					toolResult := fmt.Sprintf("Tool %q not found: %v", toolName, err)
					e.publishSessionEvent(sessionID, "message.tool.error", map[string]interface{}{
						"tool_call_id": tc.ID,
						"tool_name":    toolName,
						"error":        toolResult,
					})
					e.saveToolResult(ctx, sessionID, assistantMsg.ID, toolName, tc.ID, toolResult)
					llmMessages = append(llmMessages, llm.Message{
						Role:       "tool",
						Content:    toolResult,
						ToolCallID: tc.ID,
						Name:       toolName,
					})
					continue
				}

				// Prepare tool execution context with all dependencies.
				toolCtx := e.toolContext(ctx, agent.Name, assistantMsg.ID)

				// Apply a 30-second timeout for tool execution to prevent
				// hanging the engine loop on slow tool calls (e.g., network fetches).
				toolCtx, toolCancel := context.WithTimeout(toolCtx, 30*time.Second)
				defer toolCancel()

				// Execute the tool.
				toolResult, toolErr := tool.Execute(toolCtx, json.RawMessage(tc.Function.Arguments))
				if toolErr != nil {
					toolResult = fmt.Sprintf("Tool %q error: %v", toolName, toolErr)
					e.publishSessionEvent(sessionID, "message.tool.error", map[string]interface{}{
						"tool_call_id": tc.ID,
						"tool_name":    toolName,
						"error":        toolResult,
					})
				} else {
					e.publishSessionEvent(sessionID, "message.tool.completed", map[string]interface{}{
						"tool_call_id": tc.ID,
						"tool_name":    toolName,
						"result":       toolResult,
					})
				}

				// Save tool result message.
				e.saveToolResult(ctx, sessionID, assistantMsg.ID, toolName, tc.ID, toolResult)

				// Append tool result to in-memory context for next iteration.
				llmMessages = append(llmMessages, llm.Message{
					Role:       "tool",
					Content:    toolResult,
					ToolCallID: tc.ID,
					Name:       toolName,
				})
			}

			// Continue the loop for the next LLM call with tool results.
			continue
		}

		// --------------------------------------------------------------------
		// Case 3: Length limit or other finish reason
		// --------------------------------------------------------------------
		if choice.FinishReason == "length" {
			e.publishSessionEvent(sessionID, "message.error", map[string]string{
				"error": "LLM response truncated due to length limit",
			})
			return fmt.Errorf("engine: LLM response truncated due to length limit (iteration %d)", iteration)
		}

		// Unknown finish reason — treat as error.
		e.publishSessionEvent(sessionID, "message.error", map[string]string{
			"error": fmt.Sprintf("unexpected finish_reason: %q", choice.FinishReason),
		})
		return fmt.Errorf("engine: unexpected finish_reason: %q (iteration %d)", choice.FinishReason, iteration)
	}

	// Max iterations reached without a final "stop".
	e.publishSessionEvent(sessionID, "message.error", map[string]string{
		"error": fmt.Sprintf("max iterations (%d) reached without completion", maxIterations),
	})
	return fmt.Errorf("engine: max iterations (%d) reached without completion", maxIterations)
}

// ---------------------------------------------------------------------------
// Abort support
// ---------------------------------------------------------------------------

// Abort cancels a running Process for the given session. Returns an error if
// no process is active for the session.
func (e *Engine) Abort(sessionID string) error {
	e.abortMu.RLock()
	cancel, ok := e.abortFuncs[sessionID]
	e.abortMu.RUnlock()
	if !ok {
		return fmt.Errorf("engine: no active process for session %q", sessionID)
	}
	cancel()
	return nil
}

// setAbortFunc stores the cancel function for a session.
func (e *Engine) setAbortFunc(sessionID string, cancel context.CancelFunc) {
	e.abortMu.Lock()
	defer e.abortMu.Unlock()
	// Cancel any existing process for this session.
	if existing, ok := e.abortFuncs[sessionID]; ok {
		existing()
	}
	e.abortFuncs[sessionID] = cancel
}

// clearAbortFunc removes the cancel function for a session.
func (e *Engine) clearAbortFunc(sessionID string) {
	e.abortMu.Lock()
	defer e.abortMu.Unlock()
	delete(e.abortFuncs, sessionID)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// publishSessionEvent publishes an event to the session, if an EventBus is set.
func (e *Engine) publishSessionEvent(sessionID, eventType string, props interface{}) {
	if e.EventBus != nil {
		e.EventBus.PublishSession(sessionID, eventType, props)
	}
}

// saveToolResult persists a tool result message to the database.
func (e *Engine) saveToolResult(ctx context.Context, sessionID, parentMsgID, toolName, toolCallID, result string) {
	now := time.Now().UnixMilli()
	_, err := e.History.InsertMessage(ctx, db.CreateMessageParams{
		ID:         e.newID(),
		SessionID:  sessionID,
		ParentID:   sql.NullString{String: parentMsgID, Valid: true},
		Role:       "tool",
		Content:    sql.NullString{String: result, Valid: result != ""},
		ToolName:   sql.NullString{String: toolName, Valid: toolName != ""},
		ToolCallID: sql.NullString{String: toolCallID, Valid: toolCallID != ""},
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		// Non-fatal: log but don't abort the engine loop.
		fmt.Printf("engine: failed to save tool result for %q (call %s): %v\n", toolName, toolCallID, err)
	}
}

// getToolDefinitions returns the LLM-compatible tool definitions for the
// tools that the agent is allowed to use.
func (e *Engine) getToolDefinitions(agent *Agent) []llm.ToolDefinition {
	allDefs := e.Tools.ToToolDefinitions()
	if len(agent.Tools) == 0 {
		return nil
	}

	// Build a set of allowed tool names for this agent.
	allowed := make(map[string]bool, len(agent.Tools))
	for _, t := range agent.Tools {
		if agent.IsToolAllowed(t) {
			allowed[t] = true
		}
	}

	// Filter tools to only those the agent can use.
	filtered := make([]llm.ToolDefinition, 0, len(allowed))
	for _, td := range allDefs {
		if allowed[td.Function.Name] {
			filtered = append(filtered, llm.ToolDefinition{
				Type: td.Type,
				Function: llm.ToolFunction{
					Name:        td.Function.Name,
					Description: td.Function.Description,
					Parameters:  td.Function.Parameters,
				},
			})
		}
	}
	return filtered
}

// toolContext builds a context with all dependencies needed by tools
// (agent name, database querier, agent registry, LLM client, etc.).
func (e *Engine) toolContext(ctx context.Context, agentName, currentMsgID string) context.Context {
	toolCtx := context.WithValue(ctx, tools.ContextKeyAgentName, agentName)
	toolCtx = context.WithValue(toolCtx, tools.ContextKeyQuerier, e.Querier)
	toolCtx = context.WithValue(toolCtx, tools.ContextKeyAgentRegistry, &agentRegistryLite{r: e.Agents})
	toolCtx = context.WithValue(toolCtx, tools.ContextKeyLLMClient, &llmClientLite{c: e.LLM})
	toolCtx = context.WithValue(toolCtx, tools.ContextKeyCurrentMessageID, currentMsgID)
	return toolCtx
}

// ---------------------------------------------------------------------------
// Adapters: bridge internal packages to tools interfaces (avoid circular deps)
// ---------------------------------------------------------------------------

// llmClientLite adapts *llm.Client to the tools.LLMClientLite interface
// used by the delegate tool.
type llmClientLite struct {
	c *llm.Client
}

func (a *llmClientLite) ChatCompletion(ctx context.Context, messages []tools.LLMMessage) (string, error) {
	llmMsgs := make([]llm.Message, len(messages))
	for i, m := range messages {
		llmMsgs[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	resp, err := a.c.ChatCompletion(ctx, &llm.ChatCompletionRequest{
		Messages: llmMsgs,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// agentRegistryLite adapts *agent.Registry to the tools.AgentRegistryLite
// interface used by the delegate tool.
type agentRegistryLite struct {
	r *Registry
}

func (a *agentRegistryLite) Get(name string) (tools.AgentLite, error) {
	agent, err := a.r.Get(name)
	if err != nil {
		return nil, err
	}
	return &agentLite{a: agent}, nil
}

// agentLite adapts *agent.Agent to the tools.AgentLite interface.
type agentLite struct {
	a *Agent
}

func (a *agentLite) Name() string                   { return a.a.Name }
func (a *agentLite) SystemPrompt() string           { return a.a.SystemPrompt }
func (a *agentLite) Model() string                  { return a.a.Model }
func (a *agentLite) HasTool(name string) bool       { return a.a.HasTool(name) }
func (a *agentLite) IsToolAllowed(name string) bool { return a.a.IsToolAllowed(name) }

// ---------------------------------------------------------------------------
// ID generation (mirrors history/id.go conventions)
// ---------------------------------------------------------------------------

// newID returns a random hex string suitable as a DB primary key.
// It retries crypto/rand up to 3 times before falling back to a
// timestamp + monotonic counter to prevent collisions.
func (e *Engine) newID() string {
	b := make([]byte, 16)
	for i := 0; i < 3; i++ {
		if _, err := rand.Read(b); err == nil {
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
		}
	}
	// Fallback: timestamp + monotonic counter
	return fmt.Sprintf("engine-%d-%d", time.Now().UnixNano(), e.idCounter.Add(1))
}
