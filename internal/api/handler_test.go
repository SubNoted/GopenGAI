package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gopengai/internal/agent"
	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/history"
	"gopengai/internal/llm"
	"gopengai/internal/tools"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupHandlerDB creates an in-memory SQLite DB with migrations applied.
// Returns *sql.DB, *db.Queries, and a cleanup function.
func setupHandlerDB(t *testing.T) (*sql.DB, *db.Queries, func()) {
	t.Helper()

	sqldb, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	if err := db.Migrate(sqldb); err != nil {
		sqldb.Close()
		t.Fatalf("migrate: %v", err)
	}

	return sqldb, db.New(sqldb), func() { sqldb.Close() }
}

// seededDB creates an in-memory DB with a session and messages for tests
// that need existing data (e.g. GetSession, DeleteSession, branch ops).
// Returns *sql.DB, *db.Queries, *history.Repository, sessionID, cleanup.
func seededDB(t *testing.T) (*sql.DB, *db.Queries, *history.Repository, string, func()) {
	t.Helper()

	sqldb, q, cleanup := setupHandlerDB(t)
	repo := history.NewRepository(q, sqldb)

	now := time.Now().UnixMilli()
	session, err := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: "sess-1", AgentName: "test-agent", Title: "Test Session",
		Status: "idle", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		cleanup()
		t.Fatalf("create session: %v", err)
	}

	// Add a user message.
	_, err = repo.InsertMessage(context.Background(), db.CreateMessageParams{
		ID: "msg-1", SessionID: session.ID, Role: "user", Parts: "[]",
		Content:   sql.NullString{String: "hello", Valid: true},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		cleanup()
		t.Fatalf("create message: %v", err)
	}

	return sqldb, q, repo, session.ID, cleanup
}

// newTestHandler returns a Handler with the given DB, history, and config.
// The Engine and LLM are nil by default (tests should set them explicitly).
func newTestHandler(q db.Querier, sqldb *sql.DB, hist *history.Repository) *Handler {
	return &Handler{
		DB:       q.(*db.Queries),
		SQLDB:    sqldb,
		Config:   &config.Config{DefaultAgent: "test-agent", LLM: config.LLMConfig{Model: "test-model"}},
		History:  hist,
		EventBus: NewEventBus(),
		Wg:       &sync.WaitGroup{},
	}
}

// doRequest is a helper that sends an HTTP request to the handler and returns
// the response recorder for assertions.
func doRequest(t *testing.T, handler http.HandlerFunc, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// doRequestPV is like doRequest but also sets path values on the request.
// pathVals is alternating key, value pairs. E.g. doRequestPV(t, h, "GET", "/x/1", "",
//
//	"id", "1", "name", "test").
func doRequestPV(t *testing.T, handler http.HandlerFunc, method, path string, body string, pathVals ...string) *httptest.ResponseRecorder {
	t.Helper()
	if len(pathVals)%2 != 0 {
		t.Fatal("doRequestPV: pathVals must be even number of key,value pairs")
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i < len(pathVals); i += 2 {
		req.SetPathValue(pathVals[i], pathVals[i+1])
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// jsonBody reads and JSON-decodes the response body.
func jsonBody(t *testing.T, rec *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// callRecord is used by recordingHistoryRepo to track engine Process calls.
type callRecord struct {
	sessionID string
	content   string
	agentName string
}

// ---------------------------------------------------------------------------
// Tests: HandleHealth
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	h := &Handler{}
	rec := doRequest(t, h.HandleHealth, "GET", "/health", "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var resp map[string]string
	jsonBody(t, rec, &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want 'ok'", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleCreateSession
// ---------------------------------------------------------------------------

func TestHandleCreateSession(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	t.Run("valid request with agent_name", func(t *testing.T) {
		rec := doRequest(t, h.HandleCreateSession, "POST", "/session",
			`{"agent_name":"my-agent","title":"Chat"}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		var session SessionView
		jsonBody(t, rec, &session)
		if session.AgentName != "my-agent" {
			t.Errorf("agent_name = %q, want %q", session.AgentName, "my-agent")
		}
		if session.Title != "Chat" {
			t.Errorf("title = %q, want %q", session.Title, "Chat")
		}
		if session.Status != "idle" {
			t.Errorf("status = %q, want %q", session.Status, "idle")
		}
	})

	t.Run("empty agent_name uses default", func(t *testing.T) {
		rec := doRequest(t, h.HandleCreateSession, "POST", "/session",
			`{}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		var session SessionView
		jsonBody(t, rec, &session)
		if session.AgentName != "test-agent" {
			t.Errorf("agent_name = %q, want %q", session.AgentName, "test-agent")
		}
		// Title should be auto-generated (timestamp format).
		if session.Title == "" {
			t.Error("title should be auto-generated")
		}
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		rec := doRequest(t, h.HandleCreateSession, "POST", "/session", `{invalid}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleListSessions
// ---------------------------------------------------------------------------

func TestHandleListSessions(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	t.Run("empty list", func(t *testing.T) {
		rec := doRequest(t, h.HandleListSessions, "GET", "/session", "")
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		var sessions []SessionView
		jsonBody(t, rec, &sessions)
		if len(sessions) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(sessions))
		}
	})

	t.Run("non-empty list", func(t *testing.T) {
		// Create a session first.
		doRequest(t, h.HandleCreateSession, "POST", "/session",
			`{"agent_name":"a","title":"S1"}`)

		rec := doRequest(t, h.HandleListSessions, "GET", "/session", "")
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		var sessions []SessionView
		jsonBody(t, rec, &sessions)
		if len(sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(sessions))
		}
		if sessions[0].Title != "S1" {
			t.Errorf("title = %q", sessions[0].Title)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleGetSession
// ---------------------------------------------------------------------------

func TestHandleGetSession(t *testing.T) {
	sqldb, q, _, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	t.Run("existing session with messages", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetSession, "GET", "/session/"+sessionID, "", "id", sessionID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var session SessionView
		jsonBody(t, rec, &session)
		if session.ID != sessionID {
			t.Errorf("id = %q, want %q", session.ID, sessionID)
		}
		if len(session.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(session.Messages))
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetSession, "GET", "/session/nonexistent", "", "id", "nonexistent")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		rec := doRequest(t, h.HandleGetSession, "GET", "/session/", "")
		// Go 1.22+ routing: this won't match the path pattern without the id segment.
		// We still test the handler directly with empty id.
		req := httptest.NewRequest("GET", "/session/", nil)
		rec2 := httptest.NewRecorder()
		h.HandleGetSession(rec2, req)
		if rec2.Code == http.StatusOK {
			t.Error("expected error for missing session id")
		}
		_ = rec // suppress unused warning
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleUpdateSession
// ---------------------------------------------------------------------------

func TestHandleUpdateSession(t *testing.T) {
	sqldb, q, _, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	t.Run("valid update", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleUpdateSession, "PATCH", "/session/"+sessionID,
			`{"title":"Updated Title"}`, "id", sessionID)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var session SessionView
		jsonBody(t, rec, &session)
		if session.Title != "Updated Title" {
			t.Errorf("title = %q", session.Title)
		}
	})

	t.Run("missing title", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleUpdateSession, "PATCH", "/session/"+sessionID,
			`{}`, "id", sessionID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleUpdateSession, "PATCH", "/session/nonexistent",
			`{"title":"X"}`, "id", "nonexistent")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/session/", strings.NewReader(`{"title":"X"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleUpdateSession(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleDeleteSession
// ---------------------------------------------------------------------------

func TestHandleDeleteSession(t *testing.T) {
	sqldb, q, _, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	t.Run("valid delete", func(t *testing.T) {
		// Create a new session just for deletion.
		sess, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: "del-sess", AgentName: "a", Title: "Del", Status: "idle",
			CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
		})

		rec := doRequestPV(t, h.HandleDeleteSession, "DELETE", "/session/"+sess.ID, "", "id", sess.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]string
		jsonBody(t, rec, &resp)
		if resp["status"] != "deleted" {
			t.Errorf("status = %q", resp["status"])
		}

		// Verify it's gone.
		_, err := q.GetSessionByID(context.Background(), sess.ID)
		if err != sql.ErrNoRows {
			t.Errorf("expected session to be deleted")
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/session/", nil)
		rec := httptest.NewRecorder()
		h.HandleDeleteSession(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	_ = sessionID // used by other tests
}

// ---------------------------------------------------------------------------
// Tests: HandleChatCompletion
// ---------------------------------------------------------------------------

func TestHandleChatCompletion(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(llm.ChatCompletionResponse{
				ID:    "resp-1",
				Model: "test-model",
				Choices: []llm.Choice{{
					Index: 0,
					Message: llm.MessageResponse{
						Role:    "assistant",
						Content: "Hello!",
					},
					FinishReason: "stop",
				}},
			})
		}))
		defer server.Close()

		h := &Handler{
			LLM: &llm.Client{
				BaseURL:    server.URL,
				APIKey:     "key",
				Model:      "model",
				HTTPClient: &http.Client{},
			},
		}

		rec := doRequest(t, h.HandleChatCompletion, "POST", "/v1/chat/completions",
			`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp llm.ChatCompletionResponse
		jsonBody(t, rec, &resp)
		if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "Hello!" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("non-POST method", func(t *testing.T) {
		h := &Handler{}
		rec := doRequest(t, h.HandleChatCompletion, "GET", "/v1/chat/completions", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("empty messages array", func(t *testing.T) {
		h := &Handler{}
		rec := doRequest(t, h.HandleChatCompletion, "POST", "/v1/chat/completions",
			`{"model":"gpt-4","messages":[]}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		h := &Handler{}
		rec := doRequest(t, h.HandleChatCompletion, "POST", "/v1/chat/completions", `{bad}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("LLM server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer server.Close()

		h := &Handler{
			LLM: &llm.Client{
				BaseURL:    server.URL,
				APIKey:     "key",
				Model:      "model",
				HTTPClient: &http.Client{},
			},
		}

		rec := doRequest(t, h.HandleChatCompletion, "POST", "/v1/chat/completions",
			`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleGetSessionMessages
// ---------------------------------------------------------------------------

func TestHandleGetSessionMessages(t *testing.T) {
	sqldb, q, repo, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, repo)

	t.Run("existing session with messages", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetSessionMessages, "GET", "/session/"+sessionID+"/messages", "", "id", sessionID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var msgs []MessageView
		jsonBody(t, rec, &msgs)
		if len(msgs) != 1 {
			t.Errorf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Content != "hello" {
			t.Errorf("content = %q", msgs[0].Content)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/session//messages", nil)
		rec := httptest.NewRecorder()
		h.HandleGetSessionMessages(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleListBranches
// ---------------------------------------------------------------------------

func TestHandleListBranches(t *testing.T) {
	sqldb, q, repo, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, repo)

	t.Run("existing session with leaves", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleListBranches, "GET", "/session/"+sessionID+"/branches", "", "id", sessionID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var leaves []MessageView
		jsonBody(t, rec, &leaves)
		// msg-1 is a leaf (no children)
		if len(leaves) != 1 {
			t.Errorf("expected 1 leaf, got %d", len(leaves))
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/session//branches", nil)
		rec := httptest.NewRecorder()
		h.HandleListBranches(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleForkSession
// ---------------------------------------------------------------------------

func TestHandleForkSession(t *testing.T) {
	sqldb, q, repo, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, repo)

	t.Run("valid fork", func(t *testing.T) {
		body := fmt.Sprintf(`{"message_id":"msg-1","content":"forked content","title":"Fork","agent_name":"fork-agent"}`)
		rec := doRequestPV(t, h.HandleForkSession, "POST", "/session/"+sessionID+"/fork", body, "id", sessionID)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var session SessionView
		jsonBody(t, rec, &session)
		if session.AgentName != "fork-agent" {
			t.Errorf("agent_name = %q", session.AgentName)
		}
		if !strings.Contains(session.Title, "Fork") {
			t.Errorf("title = %q, expected 'Fork'", session.Title)
		}
	})

	t.Run("missing message_id", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleForkSession, "POST", "/session/"+sessionID+"/fork",
			`{"content":"x"}`, "id", sessionID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing content", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleForkSession, "POST", "/session/"+sessionID+"/fork",
			`{"message_id":"msg-1"}`, "id", sessionID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/session//fork", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleForkSession(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleSelectBranch
// ---------------------------------------------------------------------------

func TestHandleSelectBranch(t *testing.T) {
	sqldb, q, repo, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, repo)

	t.Run("valid selection", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleSelectBranch, "PUT", "/session/"+sessionID+"/branch",
			`{"leaf_id":"msg-1"}`, "id", sessionID)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		jsonBody(t, rec, &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q", resp["status"])
		}
	})

	t.Run("missing leaf_id", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleSelectBranch, "PUT", "/session/"+sessionID+"/branch",
			`{}`, "id", sessionID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/session//branch", strings.NewReader(`{"leaf_id":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleSelectBranch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleEditMessage
// ---------------------------------------------------------------------------

func TestHandleEditMessage(t *testing.T) {
	sqldb, q, repo, sessionID, cleanup := seededDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, repo)

	t.Run("valid edit", func(t *testing.T) {
		body := fmt.Sprintf(`{"session_id":"%s","content":"edited content","role":"user"}`, sessionID)
		rec := doRequestPV(t, h.HandleEditMessage, "PATCH", "/messages/msg-1", body, "id", "msg-1")

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var msg MessageView
		jsonBody(t, rec, &msg)
		if msg.Content != "edited content" {
			t.Errorf("content = %q", msg.Content)
		}
		if msg.Role != "user" {
			t.Errorf("role = %q", msg.Role)
		}
	})

	t.Run("default role when empty", func(t *testing.T) {
		body := fmt.Sprintf(`{"session_id":"%s","content":"edited content"}`, sessionID)
		rec := doRequestPV(t, h.HandleEditMessage, "PATCH", "/messages/msg-1", body, "id", "msg-1")

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var msg MessageView
		jsonBody(t, rec, &msg)
		if msg.Role != "user" {
			t.Errorf("role = %q, want 'user' (default)", msg.Role)
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleEditMessage, "PATCH", "/messages/msg-1",
			`{"content":"x"}`, "id", "msg-1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing content", func(t *testing.T) {
		body := fmt.Sprintf(`{"session_id":"%s"}`, sessionID)
		rec := doRequestPV(t, h.HandleEditMessage, "PATCH", "/messages/msg-1", body, "id", "msg-1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing message id", func(t *testing.T) {
		body := fmt.Sprintf(`{"session_id":"%s","content":"x"}`, sessionID)
		req := httptest.NewRequest("PATCH", "/messages/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleEditMessage(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("wrong session ID", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleEditMessage, "PATCH", "/messages/msg-1",
			`{"session_id":"wrong-sess","content":"x"}`, "id", "msg-1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleListAgents
// ---------------------------------------------------------------------------

func TestHandleListAgents(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	reg := agent.NewRegistry()
	reg.Register(&agent.Agent{Name: "a1", Model: "gpt-4", Tools: []string{"tool1"}})
	reg.Register(&agent.Agent{Name: "a2", Description: "helper"})

	h := newTestHandler(q, sqldb, nil)
	h.Engine = &agent.Engine{Agents: reg}

	t.Run("list agents", func(t *testing.T) {
		rec := doRequest(t, h.HandleListAgents, "GET", "/agents", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var agents []AgentView
		jsonBody(t, rec, &agents)
		if len(agents) != 2 {
			t.Fatalf("expected 2 agents, got %d", len(agents))
		}
		// Check first agent.
		if agents[0].Name != "a1" {
			t.Errorf("agent[0] name = %q", agents[0].Name)
		}
		if agents[0].Model != "gpt-4" {
			t.Errorf("agent[0] model = %q", agents[0].Model)
		}
	})

	t.Run("empty registry", func(t *testing.T) {
		emptyReg := agent.NewRegistry()
		h2 := newTestHandler(q, sqldb, nil)
		h2.Engine = &agent.Engine{Agents: emptyReg}

		rec := doRequest(t, h2.HandleListAgents, "GET", "/agents", "")
		var agents []AgentView
		jsonBody(t, rec, &agents)
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleGetAgent
// ---------------------------------------------------------------------------

func TestHandleGetAgent(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	reg := agent.NewRegistry()
	reg.Register(&agent.Agent{Name: "a1", SystemPrompt: "prompt", Mode: "primary"})

	h := newTestHandler(q, sqldb, nil)
	h.Engine = &agent.Engine{Agents: reg}

	t.Run("existing agent", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetAgent, "GET", "/agents/a1", "", "name", "a1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var view AgentView
		jsonBody(t, rec, &view)
		if view.Name != "a1" {
			t.Errorf("name = %q", view.Name)
		}
		if view.Mode != "primary" {
			t.Errorf("mode = %q", view.Mode)
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetAgent, "GET", "/agents/nonexistent", "", "name", "nonexistent")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("missing agent name", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/agents/", nil)
		rec := httptest.NewRecorder()
		h.HandleGetAgent(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleListMemory
// ---------------------------------------------------------------------------

func TestHandleListMemory(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	// Create an agent record (FK constraint for memory).
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "test-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})

	t.Run("empty memory list", func(t *testing.T) {
		rec := doRequest(t, h.HandleListMemory, "GET", "/memory", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var facts []MemoryView
		jsonBody(t, rec, &facts)
		if len(facts) != 0 {
			t.Errorf("expected 0 facts, got %d", len(facts))
		}
	})

	t.Run("memory with specified agent", func(t *testing.T) {
		now := time.Now().UnixMilli()
		q.CreateMemory(context.Background(), db.CreateMemoryParams{
			ID: "mem-1", AgentName: "test-agent", Key: "fact1", Value: "val1",
			CreatedAt: now, UpdatedAt: now,
		})

		rec := doRequest(t, h.HandleListMemory, "GET", "/memory?agent=test-agent", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var facts []MemoryView
		jsonBody(t, rec, &facts)
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		if facts[0].Key != "fact1" {
			t.Errorf("key = %q", facts[0].Key)
		}
		if facts[0].Value != "val1" {
			t.Errorf("value = %q", facts[0].Value)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleGetMemory
// ---------------------------------------------------------------------------

func TestHandleGetMemory(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)

	// Create agent and memory.
	q.CreateAgent(context.Background(), db.CreateAgentParams{
		Name: "test-agent", SystemPrompt: "test", Tools: "[]", Permissions: "{}", LoadedAt: 1,
	})
	now := time.Now().UnixMilli()
	q.CreateMemory(context.Background(), db.CreateMemoryParams{
		ID: "mem-1", AgentName: "test-agent", Key: "fact1", Value: "val1",
		CreatedAt: now, UpdatedAt: now,
	})

	t.Run("existing memory", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetMemory, "GET", "/memory/fact1", "", "key", "fact1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var fact MemoryView
		jsonBody(t, rec, &fact)
		if fact.Key != "fact1" {
			t.Errorf("key = %q", fact.Key)
		}
	})

	t.Run("non-existent memory", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleGetMemory, "GET", "/memory/nonexistent", "", "key", "nonexistent")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("memory with category", func(t *testing.T) {
		q.CreateMemory(context.Background(), db.CreateMemoryParams{
			ID: "mem-2", AgentName: "test-agent", Key: "cat-fact", Value: "v",
			Category:  sql.NullString{String: "general", Valid: true},
			CreatedAt: now, UpdatedAt: now,
		})
		rec := doRequestPV(t, h.HandleGetMemory, "GET", "/memory/cat-fact", "", "key", "cat-fact")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var fact MemoryView
		jsonBody(t, rec, &fact)
		if fact.Category != "general" {
			t.Errorf("category = %q, want 'general'", fact.Category)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/memory/", nil)
		rec := httptest.NewRecorder()
		h.HandleGetMemory(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleAbortSession
// ---------------------------------------------------------------------------

func TestHandleAbortSession(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)
	h.Engine = &agent.Engine{}

	t.Run("abort always returns ok even when no process", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleAbortSession, "POST", "/session/nonexistent/abort", "", "id", "nonexistent")
		// The handler logs the error but always returns OK.
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		var resp map[string]string
		jsonBody(t, rec, &resp)
		if resp["status"] != "aborted" {
			t.Errorf("status = %q", resp["status"])
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/session//abort", nil)
		rec := httptest.NewRecorder()
		h.HandleAbortSession(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleListModels
// ---------------------------------------------------------------------------

func TestHandleListModels(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	reg := agent.NewRegistry()
	reg.Register(&agent.Agent{Name: "a1", Model: "gpt-4"})
	reg.Register(&agent.Agent{Name: "a2"}) // No model — falls back to config

	h := newTestHandler(q, sqldb, nil)
	h.Config.LLM.Model = "default-model"
	h.Engine = &agent.Engine{Agents: reg}

	t.Run("returns models list", func(t *testing.T) {
		rec := doRequest(t, h.HandleListModels, "GET", "/v1/models", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp struct {
			Object string      `json:"object"`
			Data   []ModelView `json:"data"`
		}
		jsonBody(t, rec, &resp)
		if resp.Object != "list" {
			t.Errorf("object = %q", resp.Object)
		}
		// 1 default model + 2 agents = 3 models
		if len(resp.Data) != 3 {
			t.Fatalf("expected 3 models, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != "default-model" {
			t.Errorf("first model ID = %q", resp.Data[0].ID)
		}
	})

	t.Run("no agents", func(t *testing.T) {
		emptyReg := agent.NewRegistry()
		h2 := newTestHandler(q, sqldb, nil)
		h2.Config.LLM.Model = "solo-model"
		h2.Engine = &agent.Engine{Agents: emptyReg}

		rec := doRequest(t, h2.HandleListModels, "GET", "/v1/models", "")
		var resp struct {
			Data []ModelView `json:"data"`
		}
		jsonBody(t, rec, &resp)
		if len(resp.Data) != 1 {
			t.Errorf("expected 1 model (config default), got %d", len(resp.Data))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleChatMessage (sync validation)
// ---------------------------------------------------------------------------

func TestHandleChatMessage_Validation(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()
	h := newTestHandler(q, sqldb, nil)
	// Engine not set — these tests only reach the validation phase.

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/session//message",
			strings.NewReader(`{"content":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleChatMessage(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		// Create a session first.
		q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: "s1", AgentName: "test-agent", Title: "Test", Status: "idle",
			CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
		})
		rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/s1/message",
			`{"content":""}`, "id", "s1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (empty content)", rec.Code)
		}
	})

	t.Run("whitespace-only content", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/s1/message",
			`{"content":"   "}`, "id", "s1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/nonexistent/message",
			`{"content":"hello"}`, "id", "nonexistent")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: HandleChatMessage (session already processing)
// ---------------------------------------------------------------------------

func TestHandleChatMessage_AlreadyProcessing(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	now := time.Now().UnixMilli()
	q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: "s1", AgentName: "test-agent", Title: "Test", Status: "working",
		CreatedAt: now, UpdatedAt: now,
	})

	h := newTestHandler(q, sqldb, nil)

	rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/s1/message",
		`{"content":"hello"}`, "id", "s1")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (session already working)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleChatMessage (async dispatch)
// ---------------------------------------------------------------------------

func TestHandleChatMessage_AsyncDispatch(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	now := time.Now().UnixMilli()
	q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: "s1", AgentName: "test-agent", Title: "Test", Status: "idle",
		CreatedAt: now, UpdatedAt: now,
	})

	// Create a recording history + LLM server so the engine completes quickly.
	called := make(chan callRecord, 1)

	recHist := &recordingHistoryRepo{
		q:      q,
		sqldb:  sqldb,
		called: called,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(llm.ChatCompletionResponse{
			ID:    "resp-1",
			Model: "test-model",
			Choices: []llm.Choice{{
				Index: 0,
				Message: llm.MessageResponse{
					Role:    "assistant",
					Content: "Hello from engine!",
				},
				FinishReason: "stop",
			}},
			Usage: llm.Usage{TotalTokens: 10},
		})
	}))
	defer server.Close()

	reg := agent.NewRegistry()
	reg.Register(&agent.Agent{
		Name:         "test-agent",
		SystemPrompt: "You are a test assistant.",
	})

	llmClient := &llm.Client{
		BaseURL:    server.URL,
		APIKey:     "key",
		Model:      "test-model",
		HTTPClient: &http.Client{},
	}

	eng := agent.NewEngine(
		llmClient,
		tools.NewRegistry(),
		recHist,
		reg,
		q,
		&config.Config{LLM: config.LLMConfig{Model: "test-model", MaxIterations: 3}},
		nil, // EventBus — optional
	)

	h := newTestHandler(q, sqldb, nil)
	h.Engine = eng

	rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/s1/message",
		`{"content":"hello world"}`, "id", "s1")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}

	var resp map[string]string
	jsonBody(t, rec, &resp)
	if resp["status"] != "accepted" {
		t.Errorf("status = %q", resp["status"])
	}

	// Wait for the goroutine to call Process.
	select {
	case c := <-called:
		if c.sessionID != "s1" {
			t.Errorf("sessionID = %q, want %q", c.sessionID, "s1")
		}
		if c.content != "hello world" {
			t.Errorf("content = %q, want %q", c.content, "hello world")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine.Process was not called within 5s")
	}

	// Wait for the goroutine to finish (WaitGroup).
	h.Wg.Wait()
}

// ---------------------------------------------------------------------------
// recordingHistoryRepo records when Process is called and delegates to real DB.
// ---------------------------------------------------------------------------

type recordingHistoryRepo struct {
	q      *db.Queries
	sqldb  *sql.DB
	called chan callRecord
}

func (r *recordingHistoryRepo) InsertMessage(ctx context.Context, p db.CreateMessageParams) (db.Message, error) {
	// Record the call when the user message is saved (first InsertMessage call).
	select {
	case r.called <- struct {
		sessionID string
		content   string
		agentName string
	}{p.SessionID, p.Content.String, ""}:
	default:
	}
	return r.q.CreateMessage(ctx, p)
}

func (r *recordingHistoryRepo) GetSession(ctx context.Context, id string) (db.Session, error) {
	return r.q.GetSessionByID(ctx, id)
}

func (r *recordingHistoryRepo) BuildContext(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error) {
	// Return minimal context so the engine can proceed.
	return []llm.Message{{Role: "system", Content: systemPrompt}}, nil
}

func (r *recordingHistoryRepo) UpdateActiveLeaf(ctx context.Context, sessionID, leafID string) error {
	return nil
}

var _ agent.HistoryRepository = (*recordingHistoryRepo)(nil)

// ---------------------------------------------------------------------------
// Tests: toSessionView / toMessageViews / toMemoryView / toAgentView / encodeJSON
// ---------------------------------------------------------------------------

func TestToSessionView(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		s := db.Session{
			ID: "s1", AgentName: "a", Title: "T", Status: "idle",
			MessageCount: 5, CreatedAt: 1000, UpdatedAt: 2000,
		}
		msgs := []MessageView{{ID: "m1", Role: "user", Content: "hi"}}
		view := toSessionView(s, msgs)

		if view.ID != "s1" {
			t.Errorf("ID = %q", view.ID)
		}
		if view.MessageCount != 5 {
			t.Errorf("MessageCount = %d", view.MessageCount)
		}
		if len(view.Messages) != 1 {
			t.Errorf("messages = %d", len(view.Messages))
		}
	})

	t.Run("nil messages", func(t *testing.T) {
		view := toSessionView(db.Session{ID: "s1"}, nil)
		if view.Messages != nil {
			t.Errorf("messages should be nil, got %v", view.Messages)
		}
	})
}

func TestToMessageViews(t *testing.T) {
	t.Run("with content and agent_name", func(t *testing.T) {
		msgs := []db.Message{{
			ID: "m1", Role: "assistant",
			Content:   sql.NullString{String: "response", Valid: true},
			AgentName: sql.NullString{String: "a1", Valid: true},
			CreatedAt: 1000,
		}}
		views := toMessageViews(msgs)
		if len(views) != 1 {
			t.Fatalf("expected 1 view, got %d", len(views))
		}
		if views[0].Content != "response" {
			t.Errorf("content = %q", views[0].Content)
		}
		if views[0].AgentName != "a1" {
			t.Errorf("agentName = %q", views[0].AgentName)
		}
	})

	t.Run("null content", func(t *testing.T) {
		msgs := []db.Message{{ID: "m1", Role: "user"}}
		views := toMessageViews(msgs)
		if views[0].Content != "" {
			t.Errorf("content = %q, want empty", views[0].Content)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		views := toMessageViews(nil)
		if len(views) != 0 {
			t.Errorf("expected 0 views, got %d", len(views))
		}
	})
}

func TestToMemoryView(t *testing.T) {
	t.Run("with category", func(t *testing.T) {
		m := db.Memory{
			ID: "m1", AgentName: "a", Key: "k", Value: "v",
			Category:  sql.NullString{String: "general", Valid: true},
			CreatedAt: 1000, UpdatedAt: 2000,
		}
		view := toMemoryView(m)
		if view.Category != "general" {
			t.Errorf("category = %q", view.Category)
		}
	})

	t.Run("without category", func(t *testing.T) {
		m := db.Memory{ID: "m1", AgentName: "a", Key: "k", Value: "v"}
		view := toMemoryView(m)
		if view.Category != "" {
			t.Errorf("category = %q, want empty", view.Category)
		}
	})
}

func TestToAgentView(t *testing.T) {
	a := agent.Agent{
		Name: "test", Model: "gpt-4", Tools: []string{"t1", "t2"},
		ParentAgent: "parent", Description: "desc", Mode: "primary",
	}
	view := toAgentView(a)
	if view.Name != "test" {
		t.Errorf("name = %q", view.Name)
	}
	if view.Model != "gpt-4" {
		t.Errorf("model = %q", view.Model)
	}
	if len(view.Tools) != 2 {
		t.Errorf("tools = %d", len(view.Tools))
	}
}

func TestEncodeJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	encodeJSON(rec, map[string]string{"key": "val"})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), `"key"`) {
		t.Errorf("body missing key: %s", string(body))
	}
}

// ---------------------------------------------------------------------------
// Tests: newID
// ---------------------------------------------------------------------------

func TestNewID_Format(t *testing.T) {
	id1 := newID()
	id2 := newID()

	if id1 == "" {
		t.Error("newID() returned empty string")
	}
	if id1 == id2 {
		t.Errorf("newID() returned duplicate IDs: %q and %q", id1, id2)
	}
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (4 dashes).
	if strings.Count(id1, "-") != 4 {
		t.Errorf("newID() format = %q, expected 4 dashes", id1)
	}
	// Must be hex-only (except dashes).
	hexPart := strings.ReplaceAll(id1, "-", "")
	for _, c := range hexPart {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Errorf("newID() has non-hex char in %q", id1)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleChatMessage — content too large
// ---------------------------------------------------------------------------

func TestHandleChatMessage_ContentTooLarge(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	now := time.Now().UnixMilli()
	q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: "s1", AgentName: "test-agent", Title: "Test", Status: "idle",
		CreatedAt: now, UpdatedAt: now,
	})

	h := newTestHandler(q, sqldb, nil)

	// Create content > 100KB.
	tooLarge := strings.Repeat("x", maxContentLength+1)
	rec := doRequestPV(t, h.HandleChatMessage, "POST", "/session/s1/message",
		`{"content":"`+tooLarge+`"}`, "id", "s1")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: sseHeaders
// ---------------------------------------------------------------------------

func TestSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sseHeaders(rec)
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("Connection") != "keep-alive" {
		t.Errorf("Connection = %q", rec.Header().Get("Connection"))
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleGlobalSSE — basic connection open
// ---------------------------------------------------------------------------

func TestHandleGlobalSSE_Connect(t *testing.T) {
	eb := NewEventBus()
	defer eb.Close()

	h := &Handler{EventBus: eb}

	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()

	// Run in goroutine because the connection is long-lived.
	done := make(chan struct{})
	go func() {
		h.HandleGlobalSSE(rec, req)
		close(done)
	}()

	// Verify the initial ": connected" comment arrives.
	time.Sleep(50 * time.Millisecond)
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), ": connected") {
		t.Errorf("expected ': connected' comment in SSE stream, got: %q", string(body))
	}

	// Close the event bus to stop the stream.
	eb.Close()

	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("handler did not return after Close()")
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleSessionSSE — connection open
// ---------------------------------------------------------------------------

func TestHandleSessionSSE_Connect(t *testing.T) {
	eb := NewEventBus()
	defer eb.Close()

	h := &Handler{EventBus: eb}

	req := httptest.NewRequest("GET", "/session/s1/events", nil)
	req.SetPathValue("id", "s1")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.HandleSessionSSE(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), ": connected") {
		t.Errorf("expected ': connected' in SSE stream, got: %q", string(body))
	}

	eb.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after Close()")
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleSessionSSE — missing session id
// ---------------------------------------------------------------------------

func TestHandleSessionSSE_MissingID(t *testing.T) {
	h := &Handler{EventBus: NewEventBus()}
	req := httptest.NewRequest("GET", "/session//events", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionSSE(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: writeSSE
// ---------------------------------------------------------------------------

func TestWriteSSE(t *testing.T) {
	t.Run("writes valid SSE", func(t *testing.T) {
		rec := httptest.NewRecorder()
		err := writeSSE(rec, SSEEvent{Type: "test", Properties: map[string]string{"k": "v"}})
		if err != nil {
			t.Fatalf("writeSSE() error = %v", err)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "event: test") {
			t.Errorf("missing event line: %q", body)
		}
		if !strings.Contains(body, `data:`) {
			t.Errorf("missing data line: %q", body)
		}
	})

	t.Run("sanitizes newlines in event type", func(t *testing.T) {
		rec := httptest.NewRecorder()
		// Event type with embedded newline should be sanitized.
		err := writeSSE(rec, SSEEvent{Type: "ev\nent", Properties: "ok"})
		if err != nil {
			t.Fatalf("writeSSE() error = %v", err)
		}
		body := rec.Body.String()
		if strings.Contains(body, "ev\nent") {
			t.Errorf("newline not sanitized: %q", body)
		}
		if !strings.Contains(body, "event") {
			t.Errorf("event line missing: %q", body)
		}
	})

	t.Run("sanitizes newlines in data", func(t *testing.T) {
		rec := httptest.NewRecorder()
		err := writeSSE(rec, SSEEvent{Type: "e", Properties: "val\nue"})
		if err != nil {
			t.Fatalf("writeSSE() error = %v", err)
		}
		body := rec.Body.String()
		if strings.Contains(body, "val\nue") {
			t.Errorf("newline not sanitized in data: %q", body)
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: internalError (via endpoints)
// ---------------------------------------------------------------------------

func TestInternalError(t *testing.T) {
	sqldb, _, cleanup := setupHandlerDB(t)
	defer cleanup()

	// Use a closed DB to trigger internal errors.
	q := db.New(sqldb)
	sqldb.Close()

	h := &Handler{
		DB:       q,
		SQLDB:    sqldb,
		Config:   &config.Config{DefaultAgent: "test-agent"},
		EventBus: NewEventBus(),
	}

	t.Run("500 on DB error in list sessions", func(t *testing.T) {
		rec := doRequest(t, h.HandleListSessions, "GET", "/session", "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Integrations: register routes test (smoke test)
// ---------------------------------------------------------------------------

func TestRegisterRoutes(t *testing.T) {
	sqldb, q, cleanup := setupHandlerDB(t)
	defer cleanup()

	reg := agent.NewRegistry()
	reg.Register(&agent.Agent{Name: "test-agent"})

	h := &Handler{
		DB:       q,
		SQLDB:    sqldb,
		Config:   &config.Config{DefaultAgent: "test-agent", LLM: config.LLMConfig{Model: "m"}},
		History:  history.NewRepository(q, sqldb),
		EventBus: NewEventBus(),
		Engine:   &agent.Engine{Agents: reg},
		Wg:       &sync.WaitGroup{},
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, h)

	t.Run("health endpoint", func(t *testing.T) {
		rec := doRequest(t, mux.ServeHTTP, "GET", "/health", "")
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("404 for unknown path", func(t *testing.T) {
		rec := doRequest(t, mux.ServeHTTP, "GET", "/nonexistent", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("agents endpoint works", func(t *testing.T) {
		rec := doRequest(t, mux.ServeHTTP, "GET", "/agents", "")
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("create session through mux", func(t *testing.T) {
		rec := doRequest(t, mux.ServeHTTP, "POST", "/session",
			`{"agent_name":"test-agent","title":"Mux Test"}`)
		if rec.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", rec.Code)
		}
	})
}
