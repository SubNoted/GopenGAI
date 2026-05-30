package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock types for delegate test
// ---------------------------------------------------------------------------

type mockAgent struct {
	name         string
	systemPrompt string
	model        string
	tools        []string
	permissions  map[string]string
}

func (m *mockAgent) Name() string                   { return m.name }
func (m *mockAgent) SystemPrompt() string           { return m.systemPrompt }
func (m *mockAgent) Model() string                  { return m.model }
func (m *mockAgent) HasTool(name string) bool       { return m.toolsHas(name) }
func (m *mockAgent) IsToolAllowed(name string) bool { return m.permissions[name] == "allow" }
func (m *mockAgent) toolsHas(name string) bool {
	for _, t := range m.tools {
		if t == name {
			return true
		}
	}
	return false
}

type mockAgentRegistry struct {
	agents map[string]AgentLite
}

func (r *mockAgentRegistry) Get(name string) (AgentLite, error) {
	a, ok := r.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return a, nil
}

type mockLLMClient struct {
	result string
	err    error
}

func (c *mockLLMClient) ChatCompletion(ctx context.Context, messages []LLMMessage) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return c.result, nil
}

func TestDelegateTool_Execute(t *testing.T) {
	t.Run("successful delegation", func(t *testing.T) {
		reg := &mockAgentRegistry{
			agents: map[string]AgentLite{
				"researcher": &mockAgent{
					name:         "researcher",
					systemPrompt: "You are a researcher.",
				},
			},
		}
		llm := &mockLLMClient{result: "Here is my research result."}

		ctx := context.Background()
		ctx = context.WithValue(ctx, ContextKeyAgentName, "main-agent")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(llm))

		tool := &DelegateTool{}
		args := json.RawMessage(`{"agent_name":"researcher", "task":"Research Go testing"}`)

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == "" {
			t.Error("result should not be empty")
		}
	})

	t.Run("invalid JSON args", func(t *testing.T) {
		ctx := context.Background()
		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`bad`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("missing agent_name", func(t *testing.T) {
		ctx := context.Background()
		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"task":"do stuff"}`))
		if err == nil {
			t.Fatal("expected error for missing agent_name")
		}
	})

	t.Run("missing task", func(t *testing.T) {
		ctx := context.Background()
		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"x"}`))
		if err == nil {
			t.Fatal("expected error for missing task")
		}
	})

	t.Run("agent not found in registry", func(t *testing.T) {
		reg := &mockAgentRegistry{agents: map[string]AgentLite{}}
		ctx := context.WithValue(context.Background(), ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyAgentName, "main")

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"nonexistent","task":"x"}`))
		if err == nil {
			t.Fatal("expected error for non-existent agent")
		}
	})

	t.Run("missing agent registry in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(&mockLLMClient{}))

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"a","task":"x"}`))
		if err == nil {
			t.Fatal("expected error for missing registry")
		}
	})

	t.Run("missing LLM client in context", func(t *testing.T) {
		reg := &mockAgentRegistry{agents: map[string]AgentLite{
			"a": &mockAgent{name: "a", systemPrompt: "sys"},
		}}
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"a","task":"x"}`))
		if err == nil {
			t.Fatal("expected error for missing LLM client")
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		reg := &mockAgentRegistry{agents: map[string]AgentLite{
			"researcher": &mockAgent{name: "researcher", systemPrompt: "sys"},
		}}
		llm := &mockLLMClient{result: "ok"}

		// The "researcher" agent is already in the visited set.
		visited := map[string]bool{"researcher": true}
		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(llm))
		ctx = context.WithValue(ctx, ContextKeyVisitedAgents, visited)

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"researcher","task":"x"}`))
		if err == nil {
			t.Fatal("expected cycle detection error")
		}
	})

	t.Run("LLM call failure propagates error", func(t *testing.T) {
		reg := &mockAgentRegistry{agents: map[string]AgentLite{
			"a": &mockAgent{name: "a", systemPrompt: "sys"},
		}}
		llm := &mockLLMClient{err: fmt.Errorf("LLM timeout")}

		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(llm))

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"a","task":"x"}`))
		if err == nil {
			t.Fatal("expected error from LLM failure")
		}
	})

	t.Run("context timeout for sub-agent call", func(t *testing.T) {
		// The delegate tool creates a 30s sub-context internally.
		// Since we use mocks that return instantly, this just verifies
		// the plumbing works without panics.
		reg := &mockAgentRegistry{agents: map[string]AgentLite{
			"a": &mockAgent{name: "a", systemPrompt: "sys"},
		}}
		llm := &mockLLMClient{result: "ok"}

		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(llm))

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"a","task":"x"}`))
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	t.Run("logDelegation without querier (no-op)", func(t *testing.T) {
		reg := &mockAgentRegistry{agents: map[string]AgentLite{
			"a": &mockAgent{name: "a", systemPrompt: "sys"},
		}}
		llm := &mockLLMClient{result: "ok"}

		ctx := context.WithValue(context.Background(), ContextKeyAgentName, "main")
		ctx = context.WithValue(ctx, ContextKeyAgentRegistry, AgentRegistryLite(reg))
		ctx = context.WithValue(ctx, ContextKeyLLMClient, LLMClientLite(llm))
		// No Querier in context — logDelegation should be no-op.

		tool := &DelegateTool{}
		_, err := tool.Execute(ctx, json.RawMessage(`{"agent_name":"a","task":"x"}`))
		if err != nil {
			t.Fatalf("Execute() should not fail without querier: %v", err)
		}
	})

	t.Run("tool metadata", func(t *testing.T) {
		tool := &DelegateTool{}
		if tool.Name() != "delegate" {
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

func TestAgentNameFromContext(t *testing.T) {
	if name := AgentNameFromContext(context.Background()); name != "" {
		t.Errorf("empty context returned %q, want empty", name)
	}

	ctx := context.WithValue(context.Background(), ContextKeyAgentName, "test-agent")
	if name := AgentNameFromContext(ctx); name != "test-agent" {
		t.Errorf("got %q, want %q", name, "test-agent")
	}
}

func TestQuerierFromContext(t *testing.T) {
	if q := QuerierFromContext(context.Background()); q != nil {
		t.Error("empty context should return nil querier")
	}
}
