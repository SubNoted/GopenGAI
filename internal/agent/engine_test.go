package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/llm"
	"gopengai/internal/tools"
)

// ---------------------------------------------------------------------------
// Mock HistoryRepository
// ---------------------------------------------------------------------------

type mockHistoryRepo struct {
	getSessionFn       func(ctx context.Context, id string) (db.Session, error)
	insertMessageFn    func(ctx context.Context, params db.CreateMessageParams) (db.Message, error)
	buildContextFn     func(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error)
	updateActiveLeafFn func(ctx context.Context, sessionID, leafID string) error
}

func (m *mockHistoryRepo) InsertMessage(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
	if m.insertMessageFn != nil {
		return m.insertMessageFn(ctx, p)
	}
	return db.Message{ID: p.ID, Role: p.Role}, nil
}

func (m *mockHistoryRepo) GetSession(ctx context.Context, id string) (db.Session, error) {
	if m.getSessionFn != nil {
		return m.getSessionFn(ctx, id)
	}
	return db.Session{ID: id, AgentName: "test-agent", Status: "idle"}, nil
}

func (m *mockHistoryRepo) BuildContext(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error) {
	if m.buildContextFn != nil {
		return m.buildContextFn(ctx, sessionID, systemPrompt, maxTokens)
	}
	return nil, nil
}

func (m *mockHistoryRepo) UpdateActiveLeaf(ctx context.Context, sessionID, leafID string) error {
	if m.updateActiveLeafFn != nil {
		return m.updateActiveLeafFn(ctx, sessionID, leafID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock EventBus
// ---------------------------------------------------------------------------

type mockEventBus struct {
	mu            sync.Mutex
	globalEvents  []eventRecord
	sessionEvents []eventRecord
}

type eventRecord struct {
	sessionID  string
	eventType  string
	properties interface{}
}

func (m *mockEventBus) PublishGlobal(eventType string, props interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalEvents = append(m.globalEvents, eventRecord{eventType: eventType, properties: props})
}

func (m *mockEventBus) PublishSession(sessionID, eventType string, props interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionEvents = append(m.sessionEvents, eventRecord{
		sessionID: sessionID, eventType: eventType, properties: props,
	})
}

func (m *mockEventBus) lastSessionEvent() *eventRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessionEvents) == 0 {
		return nil
	}
	return &m.sessionEvents[len(m.sessionEvents)-1]
}

func (m *mockEventBus) sessionEventsByType(eventType string) []eventRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []eventRecord
	for _, e := range m.sessionEvents {
		if e.eventType == eventType {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// LLM test server
// ---------------------------------------------------------------------------

// newTestLLMServer creates an httptest server that returns canned responses.
// The handlerFn receives the request body and returns a response.
func newTestLLMServer(handlerFn func(body []byte) (int, interface{})) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)

		code, resp := handlerFn(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(resp)
	}))
}

// llmTextResponse returns a standard "stop" text response.
func llmTextResponse(content string) llm.ChatCompletionResponse {
	return llm.ChatCompletionResponse{
		ID:    "test-id",
		Model: "test-model",
		Choices: []llm.Choice{{
			Index: 0,
			Message: llm.MessageResponse{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
		Usage: llm.Usage{
			PromptTokens:     10,
			CompletionTokens: len(content),
			TotalTokens:      10 + len(content),
		},
	}
}

// llmToolCallResponse returns a tool_calls response.
func llmToolCallResponse(toolName, toolArgs string, callID string) llm.ChatCompletionResponse {
	return llm.ChatCompletionResponse{
		ID:    "test-id-tool",
		Model: "test-model",
		Choices: []llm.Choice{{
			Index: 0,
			Message: llm.MessageResponse{
				Role:    "assistant",
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:       callID,
					Type:     "function",
					Function: llm.FunctionCall{Name: toolName, Arguments: toolArgs},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// ---------------------------------------------------------------------------
// Engine builder for tests
// ---------------------------------------------------------------------------

func newTestEngine(t *testing.T, serverURL string) (*Engine, *mockHistoryRepo, *mockEventBus) {
	t.Helper()

	history := &mockHistoryRepo{}
	events := &mockEventBus{}

	reg := NewRegistry()
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.MemorySave{})

	reg.Register(&Agent{
		Name:         "test-agent",
		SystemPrompt: "You are a test assistant.",
		Tools:        []string{"memory_save"},
		Permissions:  map[string]string{"memory_save": "allow"},
	})

	llmClient := &llm.Client{
		BaseURL:    serverURL,
		APIKey:     "test-key",
		Model:      "test-model",
		HTTPClient: &http.Client{},
	}

	return &Engine{
		LLM:     llmClient,
		Tools:   toolReg,
		History: history,
		Agents:  reg,
		Config: &config.Config{
			LLM: config.LLMConfig{Model: "test-model", MaxIterations: 3},
		},
		EventBus:   events,
		abortFuncs: make(map[string]context.CancelFunc),
	}, history, events
}

// ---------------------------------------------------------------------------
// Tests: newID
// ---------------------------------------------------------------------------

func TestEngineNewID(t *testing.T) {
	e := &Engine{}
	id1 := e.newID()
	id2 := e.newID()

	if id1 == "" {
		t.Error("newID() returned empty string")
	}
	if id1 == id2 {
		t.Errorf("newID() returned duplicate IDs: %q and %q", id1, id2)
	}
	// Must contain at least 3 dashes (UUID format: xxxx-xx-xx-xx-xxxxxx).
	if strings.Count(id1, "-") < 3 {
		t.Errorf("newID() format = %q, expected UUID-like", id1)
	}
}

// ---------------------------------------------------------------------------
// Tests: getToolDefinitions
// ---------------------------------------------------------------------------

func TestGetToolDefinitions(t *testing.T) {
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.MemorySave{})

	e := &Engine{Tools: toolReg}

	t.Run("agent with no tools returns nil", func(t *testing.T) {
		agent := &Agent{Name: "a", Tools: nil}
		defs := e.getToolDefinitions(agent)
		if defs != nil {
			t.Errorf("expected nil defs for agent with no tools, got %d", len(defs))
		}
	})

	t.Run("agent with allowed tool", func(t *testing.T) {
		agent := &Agent{
			Name:        "a",
			Tools:       []string{"memory_save"},
			Permissions: map[string]string{"memory_save": "allow"},
		}
		defs := e.getToolDefinitions(agent)
		if len(defs) != 1 {
			t.Fatalf("expected 1 tool def, got %d", len(defs))
		}
		if defs[0].Function.Name != "memory_save" {
			t.Errorf("tool name = %q, want memory_save", defs[0].Function.Name)
		}
	})

	t.Run("agent with denied tool", func(t *testing.T) {
		agent := &Agent{
			Name:        "a",
			Tools:       []string{"memory_save"},
			Permissions: map[string]string{"memory_save": "deny"},
		}
		defs := e.getToolDefinitions(agent)
		if len(defs) != 0 {
			t.Errorf("expected 0 defs for denied tool, got %d", len(defs))
		}
	})

	t.Run("agent with tool not in permissions map", func(t *testing.T) {
		agent := &Agent{
			Name:        "a",
			Tools:       []string{"memory_save"},
			Permissions: map[string]string{},
		}
		defs := e.getToolDefinitions(agent)
		// Empty permissions means "all tools in the list are allowed" (IsToolAllowed returns HasTool).
		if len(defs) != 1 {
			t.Errorf("expected 1 def when no permission (empty=allow all), got %d", len(defs))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: Abort
// ---------------------------------------------------------------------------

func TestEngineAbort(t *testing.T) {
	e := &Engine{abortFuncs: make(map[string]context.CancelFunc)}

	t.Run("abort non-existent session", func(t *testing.T) {
		err := e.Abort("no-such-session")
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
	})

	t.Run("abort active session", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		e.setAbortFunc("s1", cancel)
		if err := e.Abort("s1"); err != nil {
			t.Fatalf("Abort() error = %v", err)
		}
		// Context should be cancelled.
		if ctx.Err() == nil {
			t.Error("context was not cancelled")
		}
	})

	t.Run("double abort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		e.setAbortFunc("s2", cancel)
		e.Abort("s2")
		// Second abort: the cancel func is still in the map (clearAbortFunc
		// is only called from Process's defer, not from Abort).
		// Calling cancel on an already-cancelled context is safe (no-op).
		// Abort will find it and call cancel again, returning nil.
		err := e.Abort("s2")
		if err != nil {
			t.Fatalf("second abort should not error (cancel func still present): %v", err)
		}
		_ = ctx.Err() // context already cancelled
	})

	t.Run("setAbortFunc cancels existing", func(t *testing.T) {
		ctx1, cancel1 := context.WithCancel(context.Background())
		e.setAbortFunc("s3", cancel1)

		ctx2, cancel2 := context.WithCancel(context.Background())
		e.setAbortFunc("s3", cancel2) // should cancel ctx1

		if ctx1.Err() == nil {
			t.Error("ctx1 was not cancelled when replaced")
		}
		_ = ctx2.Err() // ctx2 should still be active
	})
}

// ---------------------------------------------------------------------------
// Tests: agentLite adapter
// ---------------------------------------------------------------------------

func TestAgentLite(t *testing.T) {
	agent := &Agent{
		Name:         "test",
		SystemPrompt: "prompt",
		Model:        "gpt-4",
		Tools:        []string{"tool1"},
		Permissions: map[string]string{
			"tool1": "allow",
			"tool2": "deny",
		},
	}
	al := &agentLite{a: agent}

	t.Run("Name", func(t *testing.T) {
		if al.Name() != "test" {
			t.Errorf("Name() = %q", al.Name())
		}
	})
	t.Run("SystemPrompt", func(t *testing.T) {
		if al.SystemPrompt() != "prompt" {
			t.Errorf("SystemPrompt() = %q", al.SystemPrompt())
		}
	})
	t.Run("Model", func(t *testing.T) {
		if al.Model() != "gpt-4" {
			t.Errorf("Model() = %q", al.Model())
		}
	})
	t.Run("HasTool", func(t *testing.T) {
		if !al.HasTool("tool1") {
			t.Error("HasTool(tool1) should be true")
		}
		if al.HasTool("tool2") {
			t.Error("HasTool(tool2) should be false")
		}
	})
	t.Run("IsToolAllowed", func(t *testing.T) {
		if !al.IsToolAllowed("tool1") {
			t.Error("IsToolAllowed(tool1) should be true")
		}
		if al.IsToolAllowed("tool2") {
			t.Error("IsToolAllowed(tool2) should be false")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: Process — error cases
// ---------------------------------------------------------------------------

func TestProcess_EmptyContent(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, _, _ := newTestEngine(t, server.URL)
	err := e.Process(context.Background(), "s1", "", "test-agent")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestProcess_AgentNotFound(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, _, _ := newTestEngine(t, server.URL)
	err := e.Process(context.Background(), "s1", "hello", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
}

func TestProcess_SessionNotFound(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, history, _ := newTestEngine(t, server.URL)
	history.getSessionFn = func(ctx context.Context, id string) (db.Session, error) {
		return db.Session{}, sql.ErrNoRows
	}

	err := e.Process(context.Background(), "s1", "hello", "test-agent")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(err.Error(), "session") {
		t.Errorf("error message should mention session: %v", err)
	}
}

func TestProcess_BuildContextError(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)
	history.buildContextFn = func(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error) {
		return nil, fmt.Errorf("build context failed")
	}

	err := e.Process(context.Background(), "s1", "hello", "test-agent")
	if err == nil {
		t.Fatal("expected error for build context failure")
	}

	// Check error event was published.
	errEvents := events.sessionEventsByType("message.error")
	if len(errEvents) == 0 {
		t.Error("expected message.error event")
	}
}

func TestProcess_LLMError(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 500, map[string]interface{}{
			"error": map[string]interface{}{
				"message": "internal server error",
				"type":    "server_error",
			},
		}
	})
	defer server.Close()

	e, _, events := newTestEngine(t, server.URL)
	err := e.Process(context.Background(), "s1", "hello", "test-agent")
	if err == nil {
		t.Fatal("expected error for LLM failure")
	}

	errEvents := events.sessionEventsByType("message.error")
	if len(errEvents) == 0 {
		t.Error("expected message.error event on LLM failure")
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — successful text response
// ---------------------------------------------------------------------------

func TestProcess_SuccessfulTextResponse(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("Hello, I'm a test bot!")
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)

	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "hello", "test-agent")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Check session status events.
	statusEvents := events.sessionEventsByType("session.status")
	if len(statusEvents) < 2 {
		t.Fatalf("expected at least 2 session.status events, got %d", len(statusEvents))
	}

	// Check message.complete event.
	completeEvents := events.sessionEventsByType("message.complete")
	if len(completeEvents) != 1 {
		t.Fatalf("expected 1 message.complete event, got %d", len(completeEvents))
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — tool calling
// ---------------------------------------------------------------------------

func TestProcess_ToolCalling(t *testing.T) {
	// Set up in-memory DB because MemorySave tool needs a Querier.
	sqldb, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	defer sqldb.Close()
	if err := db.Migrate(sqldb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	querier := db.New(sqldb)

	// Create the agent record needed by Memory FK constraint.
	querier.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "test-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})

	callCount := 0
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		callCount++
		if callCount == 1 {
			return 200, llmToolCallResponse("memory_save", `{"key":"test","value":"data"}`, "call-1")
		}
		return 200, llmTextResponse("Saved!")
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)
	e.Querier = querier

	var savedMsgs []db.CreateMessageParams
	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		savedMsgs = append(savedMsgs, p)
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err = e.Process(context.Background(), "s1", "save data", "test-agent")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// We should have more than 2 messages: user + assistant (tool call) + tool result + assistant (text).
	if len(savedMsgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(savedMsgs))
	}

	// Check tool events.
	toolStarted := events.sessionEventsByType("message.tool.started")
	if len(toolStarted) == 0 {
		t.Error("expected message.tool.started event")
	}
	toolCompleted := events.sessionEventsByType("message.tool.completed")
	if len(toolCompleted) == 0 {
		t.Error("expected message.tool.completed event")
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — tool not allowed
// ---------------------------------------------------------------------------

func TestProcess_ToolNotAllowed(t *testing.T) {
	callCount := 0
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		callCount++
		if callCount == 1 {
			// Request a tool the agent doesn't have permission for.
			return 200, llmToolCallResponse("web_fetch", `{"url":"http://example.com"}`, "call-1")
		}
		return 200, llmTextResponse("done")
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)

	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "fetch something", "test-agent")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Should have a tool.error event since web_fetch is not allowed.
	toolErrors := events.sessionEventsByType("message.tool.error")
	if len(toolErrors) == 0 {
		t.Error("expected message.tool.error event for unauthorized tool")
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — tool not found in registry
// ---------------------------------------------------------------------------

func TestProcess_ToolNotFoundInRegistry(t *testing.T) {
	callCount := 0
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		callCount++
		if callCount == 1 {
			return 200, llmToolCallResponse("nonexistent_tool", `{}`, "call-1")
		}
		return 200, llmTextResponse("done")
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)

	// Allow the tool in agent permissions so we reach the registry lookup.
	e.Agents.Register(&Agent{
		Name:         "test-agent",
		SystemPrompt: "test",
		Tools:        []string{"nonexistent_tool"},
		Permissions:  map[string]string{"nonexistent_tool": "allow"},
	})

	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "use bad tool", "test-agent")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	toolErrors := events.sessionEventsByType("message.tool.error")
	if len(toolErrors) == 0 {
		t.Error("expected message.tool.error event for unknown tool")
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — max iterations
// ---------------------------------------------------------------------------

func TestProcess_MaxIterations(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		// Always return tool_calls, never stop -> should hit max iterations.
		return 200, llmToolCallResponse("memory_save", `{"key":"x","value":"y"}`, "call-1")
	})
	defer server.Close()

	e, history, _ := newTestEngine(t, server.URL)
	e.Config.LLM.MaxIterations = 2

	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "loop", "test-agent")
	if err == nil {
		t.Fatal("expected error for max iterations")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Errorf("error = %v, want max iterations", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — context cancellation
// ---------------------------------------------------------------------------

func TestProcess_ContextCancellation(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		time.Sleep(100 * time.Millisecond)
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, history, _ := newTestEngine(t, server.URL)
	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := e.Process(ctx, "s1", "hello", "test-agent")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Tests: agentRegistryLite adapter
// ---------------------------------------------------------------------------

func TestAgentRegistryLite(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "a", SystemPrompt: "sp", Model: "m"})

	al := &agentRegistryLite{r: reg}

	t.Run("Get existing agent", func(t *testing.T) {
		agent, err := al.Get("a")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if agent.Name() != "a" {
			t.Errorf("name = %q", agent.Name())
		}
	})

	t.Run("Get non-existent agent", func(t *testing.T) {
		_, err := al.Get("nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent agent")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: llmClientLite adapter
// ---------------------------------------------------------------------------

func TestLLMClientLite(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llmTextResponse("response text")
	})
	defer server.Close()

	client := &llm.Client{
		BaseURL:    server.URL,
		APIKey:     "key",
		Model:      "model",
		HTTPClient: &http.Client{},
	}
	adapter := &llmClientLite{c: client}

	result, err := adapter.ChatCompletion(context.Background(), []tools.LLMMessage{
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if result != "response text" {
		t.Errorf("result = %q, want 'response text'", result)
	}
}

// ---------------------------------------------------------------------------
// Tests: toolContext
// ---------------------------------------------------------------------------

func TestToolContext(t *testing.T) {
	// Set up in-memory DB so Querier is non-nil.
	sqldb, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	defer sqldb.Close()
	querier := db.New(sqldb)

	e := &Engine{Querier: querier}
	ctx := e.toolContext(context.Background(), "test-agent", "msg-1")

	// Check all context values are set.
	if v := tools.AgentNameFromContext(ctx); v != "test-agent" {
		t.Errorf("AgentName = %q", v)
	}
	if v := tools.QuerierFromContext(ctx); v == nil {
		t.Error("Querier context value not set")
	}
	if v, ok := ctx.Value(tools.ContextKeyCurrentMessageID).(string); !ok || v != "msg-1" {
		t.Errorf("CurrentMessageID = %q", v)
	}
}

// ---------------------------------------------------------------------------
// Tests: Process — finish_reason "length"
// ---------------------------------------------------------------------------

func TestProcess_FinishReasonLength(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		return 200, llm.ChatCompletionResponse{
			ID:    "test",
			Model: "model",
			Choices: []llm.Choice{{
				Index: 0,
				Message: llm.MessageResponse{
					Role:    "assistant",
					Content: "partial response",
				},
				FinishReason: "length",
			}},
			Usage: llm.Usage{TotalTokens: 100},
		}
	})
	defer server.Close()

	e, history, events := newTestEngine(t, server.URL)
	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		// Skip LLM error for length — engine handles this.
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "hello", "test-agent")
	if err == nil {
		t.Fatal("expected error for length finish reason")
	}
	if !strings.Contains(err.Error(), "length limit") {
		t.Errorf("error = %v, want length limit", err)
	}

	errEvents := events.sessionEventsByType("message.error")
	if len(errEvents) == 0 {
		t.Error("expected message.error event")
	}
}

// ---------------------------------------------------------------------------
// Tests: setting model from config when agent model is empty
// ---------------------------------------------------------------------------

func TestProcess_FallsBackToConfigModel(t *testing.T) {
	server := newTestLLMServer(func(body []byte) (int, interface{}) {
		// Check that the request uses the config model, not agent model.
		var req llm.ChatCompletionRequest
		json.Unmarshal(body, &req)
		if req.Model != "config-model" {
			return 500, map[string]string{"error": fmt.Sprintf("wrong model: %s", req.Model)}
		}
		return 200, llmTextResponse("ok")
	})
	defer server.Close()

	e, history, _ := newTestEngine(t, server.URL)
	e.Config.LLM.Model = "config-model"
	e.Agents.Register(&Agent{
		Name:         "no-model-agent",
		SystemPrompt: "test",
		// Model is empty, should fall back to config.
	})

	history.insertMessageFn = func(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
		return db.Message{ID: p.ID, Role: p.Role}, nil
	}

	err := e.Process(context.Background(), "s1", "hello", "no-model-agent")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
}
