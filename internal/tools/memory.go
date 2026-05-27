package tools

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"gopengai/internal/db"
)

// ---------------------------------------------------------------------------
// Context keys for tool execution
// ---------------------------------------------------------------------------

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const (
	// ContextKeyAgentName is the context key for the current agent name.
	ContextKeyAgentName contextKey = "agent_name"

	// ContextKeyQuerier is the context key for the db.Querier used by tools.
	ContextKeyQuerier contextKey = "db_querier"
)

// AgentNameFromContext extracts the agent name from the context.
// Returns empty string if not set.
func AgentNameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyAgentName).(string); ok {
		return v
	}
	return ""
}

// QuerierFromContext extracts the db.Querier from the context.
// Returns nil if not set.
func QuerierFromContext(ctx context.Context) db.Querier {
	if v, ok := ctx.Value(ContextKeyQuerier).(db.Querier); ok {
		return v
	}
	return nil
}

// ---------------------------------------------------------------------------
// MemorySave tool
// ---------------------------------------------------------------------------

// MemorySave saves a memory fact for the current agent.
type MemorySave struct{}

// Name returns the tool name.
func (m *MemorySave) Name() string { return "memory_save" }

// Description returns a human-readable description.
func (m *MemorySave) Description() string {
	return "Save a fact, preference, or piece of information to the agent's long-term memory. " +
		"The memory is scoped to the current agent and can be recalled later using memory_recall."
}

// Parameters returns the JSON Schema for the tool's arguments.
func (m *MemorySave) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"key": {
				"type": "string",
				"description": "A unique key for the memory fact (e.g. 'user_name', 'project_preference')"
			},
			"value": {
				"type": "string",
				"description": "The value or content of the memory fact"
			},
			"category": {
				"type": "string",
				"description": "Optional category for grouping related memories (e.g. 'user_info', 'project', 'preference')"
			}
		},
		"required": ["key", "value"]
	}`)
}

// Execute saves a memory fact to the database.
func (m *MemorySave) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Category string `json:"category,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("memory_save: invalid arguments: %w", err)
	}
	if params.Key == "" {
		return "", fmt.Errorf("memory_save: key is required")
	}
	if params.Value == "" {
		return "", fmt.Errorf("memory_save: value is required")
	}

	agentName := AgentNameFromContext(ctx)
	if agentName == "" {
		return "", fmt.Errorf("memory_save: agent name not set in context")
	}

	q := QuerierFromContext(ctx)
	if q == nil {
		return "", fmt.Errorf("memory_save: database querier not set in context")
	}

	now := time.Now().UnixMilli()
	var cat sql.NullString
	if params.Category != "" {
		cat = sql.NullString{String: params.Category, Valid: true}
	}
	_, err := q.CreateMemory(ctx, db.CreateMemoryParams{
		ID:        newToolID(),
		AgentName: agentName,
		Key:       params.Key,
		Value:     params.Value,
		Category:  cat,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return "", fmt.Errorf("memory_save: save failed: %w", err)
	}

	return fmt.Sprintf("Saved memory fact '%s' for agent '%s'", params.Key, agentName), nil
}

// ---------------------------------------------------------------------------
// MemoryRecall tool
// ---------------------------------------------------------------------------

// MemoryRecall retrieves memory facts for the current agent.
// If key is provided, it returns the specific fact. If key is empty,
// it returns all facts for the agent.
type MemoryRecall struct{}

// Name returns the tool name.
func (m *MemoryRecall) Name() string { return "memory_recall" }

// Description returns a human-readable description.
func (m *MemoryRecall) Description() string {
	return "Recall previously saved memory facts for the current agent. " +
		"Provide a specific key to retrieve a single fact, or leave key empty to list all facts."
}

// Parameters returns the JSON Schema for the tool's arguments.
func (m *MemoryRecall) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"key": {
				"type": "string",
				"description": "The key of the memory fact to recall. If empty, all facts are returned."
			}
		}
	}`)
}

// Execute retrieves memory facts from the database.
func (m *MemoryRecall) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Key string `json:"key"`
	}
	// Unmarshal errors are non-fatal — key is optional.
	if err := json.Unmarshal(args, &params); err != nil {
		// Malformed JSON: treat as missing key (list all).
		params.Key = ""
	}

	agentName := AgentNameFromContext(ctx)
	if agentName == "" {
		return "", fmt.Errorf("memory_recall: agent name not set in context")
	}

	q := QuerierFromContext(ctx)
	if q == nil {
		return "", fmt.Errorf("memory_recall: database querier not set in context")
	}

	if params.Key != "" {
		// Recall a specific fact.
		mem, err := q.GetMemory(ctx, db.GetMemoryParams{
			AgentName: agentName,
			Key:       params.Key,
		})
		if err != nil {
			return "", fmt.Errorf("memory_recall: fact '%s' not found: %w", params.Key, err)
		}
		return fmt.Sprintf("Key: %s\nValue: %s\nCategory: %s", mem.Key, mem.Value, nullStringVal(mem.Category)), nil
	}

	// List all facts for this agent.
	memories, err := q.ListMemoryByAgent(ctx, agentName)
	if err != nil {
		return "", fmt.Errorf("memory_recall: list failed: %w", err)
	}

	if len(memories) == 0 {
		return "No memory facts found for this agent.", nil
	}

	var result string
	for i, mem := range memories {
		if i > 0 {
			result += "\n---\n"
		}
		result += fmt.Sprintf("Key: %s\nValue: %s\nCategory: %s", mem.Key, mem.Value, nullStringVal(mem.Category))
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newToolID returns a unique ID for database records created by tools.
// Combines timestamp with random hex to avoid collisions under concurrency.
// Shared with delegate.go and any other tool that creates DB records.
func newToolID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: just use timestamp (rare, only on crypto failure)
		return fmt.Sprintf("tool-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("tool-%d-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

// nullStringVal returns the value of a sql.NullString, or "" if not valid.
func nullStringVal(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
