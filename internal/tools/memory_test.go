package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"gopengai/internal/db"
)

// setupMemoryDB creates an in-memory SQLite database with migrations applied,
// then returns a *sql.DB and *db.Queries.
func setupMemoryDB(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()

	sqldb, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}

	if err := db.Migrate(sqldb); err != nil {
		sqldb.Close()
		t.Fatalf("migrate: %v", err)
	}

	return sqldb, db.New(sqldb)
}

func TestMemorySave_Execute(t *testing.T) {
	sqldb, q := setupMemoryDB(t)
	defer sqldb.Close()

	// Create agent record needed by FK constraint.
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "test-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "overwrite-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})

	t.Run("save a memory fact", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "test-agent")
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}
		args := json.RawMessage(`{"key":"user_name", "value":"Alice"}`)

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == "" {
			t.Error("result should not be empty")
		}

		// Verify the fact was saved.
		mem, err := q.GetMemory(context.Background(), db.GetMemoryParams{
			AgentName: "test-agent",
			Key:       "user_name",
		})
		if err != nil {
			t.Fatalf("GetMemory() error = %v", err)
		}
		if mem.Value != "Alice" {
			t.Errorf("saved value = %q, want %q", mem.Value, "Alice")
		}
	})

	t.Run("save with category", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "test-agent")
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}
		args := json.RawMessage(`{"key":"project_lang", "value":"Go", "category":"preference"}`)

		_, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		mem, _ := q.GetMemory(context.Background(), db.GetMemoryParams{
			AgentName: "test-agent",
			Key:       "project_lang",
		})
		cat := ""
		if mem.Category.Valid {
			cat = mem.Category.String
		}
		if cat != "preference" {
			t.Errorf("category = %q, want %q", cat, "preference")
		}
	})

	t.Run("missing key returns error", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "agent")
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"value":"x"}`))
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("missing value returns error", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "agent")
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"key":"k"}`))
		if err == nil {
			t.Fatal("expected error for missing value")
		}
	})

	t.Run("missing agent name in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyQuerier, q)

		tool := &MemorySave{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"key":"k","value":"v"}`))
		if err == nil {
			t.Fatal("expected error for missing agent name")
		}
	})

	t.Run("missing querier in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "agent")

		tool := &MemorySave{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"key":"k","value":"v"}`))
		if err == nil {
			t.Fatal("expected error for missing querier")
		}
	})

	t.Run("invalid JSON args", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "agent")
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}
		_, err := tool.Execute(ctx, json.RawMessage(`bad`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("overwrite existing key", func(t *testing.T) {
		agentName := "overwrite-agent"
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, agentName)
		ctx = context.WithValue(ctx, ContextKeyQuerier, q)

		tool := &MemorySave{}

		// Save initial.
		tool.Execute(ctx, json.RawMessage(`{"key":"fav_color", "value":"blue"}`))
		// Overwrite.
		tool.Execute(ctx, json.RawMessage(`{"key":"fav_color", "value":"green"}`))

		mem, err := q.GetMemory(context.Background(), db.GetMemoryParams{
			AgentName: agentName,
			Key:       "fav_color",
		})
		if err != nil {
			t.Fatalf("GetMemory() error = %v", err)
		}
		if mem.Value != "green" {
			t.Errorf("overwritten value = %q, want %q", mem.Value, "green")
		}
	})

	t.Run("tool metadata", func(t *testing.T) {
		tool := &MemorySave{}
		if tool.Name() != "memory_save" {
			t.Errorf("Name() = %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() is empty")
		}
		if len(tool.Parameters()) == 0 {
			t.Error("Parameters() is empty")
		}
	})
}

func TestMemoryRecall_Execute(t *testing.T) {
	sqldb, q := setupMemoryDB(t)
	defer sqldb.Close()

	// Create agents needed by FK constraint.
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "recall-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "no-memory-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})

	// Prepopulate some memory facts.
	ctx := context.WithValue(context.Background(), ContextKeyAgentName, "recall-agent")
	ctx = context.WithValue(ctx, ContextKeyQuerier, q)

	save := &MemorySave{}
	save.Execute(ctx, json.RawMessage(`{"key":"fact1","value":"value1","category":"cat1"}`))
	save.Execute(ctx, json.RawMessage(`{"key":"fact2","value":"value2","category":"cat2"}`))

	t.Run("recall specific fact", func(t *testing.T) {
		tool := &MemoryRecall{}
		result, err := tool.Execute(ctx, json.RawMessage(`{"key":"fact1"}`))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == "" {
			t.Error("result should not be empty")
		}
	})

	t.Run("recall non-existent fact", func(t *testing.T) {
		tool := &MemoryRecall{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"key":"nope"}`))
		if err == nil {
			t.Fatal("expected error for non-existent fact")
		}
	})

	t.Run("list all facts (empty key)", func(t *testing.T) {
		tool := &MemoryRecall{}
		result, err := tool.Execute(ctx, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == "" {
			t.Error("result should not be empty (should list facts)")
		}
	})

	t.Run("list all facts (null key)", func(t *testing.T) {
		tool := &MemoryRecall{}
		result, err := tool.Execute(ctx, json.RawMessage(`{"key":null}`))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == "" {
			t.Error("result should not be empty")
		}
	})

	t.Run("no facts for agent", func(t *testing.T) {
		agentCtx := context.WithValue(context.Background(), ContextKeyAgentName, "no-memory-agent")
		agentCtx = context.WithValue(agentCtx, ContextKeyQuerier, q)

		tool := &MemoryRecall{}
		result, err := tool.Execute(agentCtx, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result != "No memory facts found for this agent." {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("missing agent name in context", func(t *testing.T) {
		badCtx := context.WithValue(context.Background(), ContextKeyQuerier, q)
		tool := &MemoryRecall{}
		_, err := tool.Execute(badCtx, json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error for missing agent name")
		}
	})

	t.Run("missing querier in context", func(t *testing.T) {
		badCtx := context.WithValue(context.Background(), ContextKeyAgentName, "agent")
		tool := &MemoryRecall{}
		_, err := tool.Execute(badCtx, json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error for missing querier")
		}
	})

	t.Run("malformed JSON for key", func(t *testing.T) {
		tool := &MemoryRecall{}
		// Malformed JSON is treated as "no key" (list all).
		result, err := tool.Execute(ctx, json.RawMessage(`bad`))
		if err != nil {
			t.Fatalf("Execute() should not fail on malformed JSON: %v", err)
		}
		if result == "" {
			t.Error("should return list of facts")
		}
	})

	t.Run("tool metadata", func(t *testing.T) {
		tool := &MemoryRecall{}
		if tool.Name() != "memory_recall" {
			t.Errorf("Name() = %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() is empty")
		}
		if len(tool.Parameters()) == 0 {
			t.Error("Parameters() is empty")
		}
	})
}
