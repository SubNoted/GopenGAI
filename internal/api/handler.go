package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/llm"
)

// Handler holds the dependencies for HTTP request handlers.
type Handler struct {
	LLM    *llm.Client
	DB     *db.Queries
	SQLDB  *sql.DB
	Config *config.Config
}

// ---------------------------------------------------------------------------
// UUID generation (stdlib only — no uuid library dependency)
// ---------------------------------------------------------------------------

// newID returns a random hex string suitable as a DB primary key.
// On crypto/rand failure, it logs the error and falls back to a timestamp-based ID.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("crypto/rand failed, using fallback ID: %v", err)
		// Fallback: timestamp + nanosecond counter — not UUID, but unique enough
		now := time.Now()
		return fmt.Sprintf("fallback-%x-%x", now.UnixMilli(), now.Nanosecond())
	}
	// Format as: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------

// internalError logs the real error and sends a generic 500 to the client.
// This prevents leaking internal details (SQL schema, file paths, etc.).
func (h *Handler) internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

// ChatRequest is the request body from a client hitting the chat endpoint.
type ChatRequest struct {
	Model      string               `json:"model"`
	Messages   []llm.Message        `json:"messages"`
	Tools      []llm.ToolDefinition `json:"tools,omitempty"`
	ToolChoice json.RawMessage      `json:"tool_choice,omitempty"`
}

// CreateSessionRequest is the request body for POST /session.
type CreateSessionRequest struct {
	Title     string `json:"title,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

// ChatResponse is the JSON body returned from POST /session/{id}/message.
type ChatResponse struct {
	SessionID string     `json:"session_id"`
	MessageID string     `json:"message_id"`
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Model     string     `json:"model"`
	Usage     *llm.Usage `json:"usage,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// SessionView is a JSON-friendly session representation for API responses.
type SessionView struct {
	ID           string        `json:"id"`
	AgentName    string        `json:"agent_name"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	MessageCount int64         `json:"message_count"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
	Messages     []MessageView `json:"messages,omitempty"`
}

// MessageView is a JSON-friendly message representation.
type MessageView struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	AgentName string `json:"agent_name,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Session endpoints
// ---------------------------------------------------------------------------

// HandleCreateSession handles POST /session.
func (h *Handler) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.AgentName == "" {
		req.AgentName = h.Config.DefaultAgent
	}
	if req.Title == "" {
		req.Title = "Chat " + time.Now().Format("2006-01-02 15:04")
	}

	now := time.Now().UnixMilli()
	session, err := h.DB.CreateSession(r.Context(), db.CreateSessionParams{
		ID:        newID(),
		AgentName: req.AgentName,
		Title:     req.Title,
		Status:    "idle",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		h.internalError(w, fmt.Errorf("create session: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toSessionView(session, nil))
}

// HandleListSessions handles GET /session.
func (h *Handler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.DB.ListSessions(r.Context())
	if err != nil {
		h.internalError(w, fmt.Errorf("list sessions: %w", err))
		return
	}

	views := make([]SessionView, 0, len(sessions))
	for _, s := range sessions {
		views = append(views, toSessionView(s, nil))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(views)
}

// HandleGetSession handles GET /session/{id}.
func (h *Handler) HandleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	session, err := h.DB.GetSessionByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		h.internalError(w, fmt.Errorf("get session: %w", err))
		return
	}

	messages, err := h.DB.ListMessagesBySession(r.Context(), id)
	if err != nil {
		h.internalError(w, fmt.Errorf("list messages: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toSessionView(session, toMessageViews(messages)))
}

// HandleDeleteSession handles DELETE /session/{id}.
func (h *Handler) HandleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	// Delete messages first (FK constraint).
	if err := h.DB.DeleteSessionMessages(r.Context(), id); err != nil {
		h.internalError(w, fmt.Errorf("delete messages: %w", err))
		return
	}
	if err := h.DB.DeleteSession(r.Context(), id); err != nil {
		h.internalError(w, fmt.Errorf("delete session: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Chat endpoint (history-aware)
// ---------------------------------------------------------------------------

// HandleChatMessage handles POST /session/{id}/message.
// It saves the user message, calls the LLM with full session context,
// saves the assistant response, and returns it.
//
// The initial DB operations (read session + write user message + update status)
// are wrapped in a transaction to prevent race conditions where concurrent
// requests to the same session could create sibling messages off the same parent.
// The LLM call happens outside the transaction to avoid holding a DB lock
// during a potentially slow HTTP request.
func (h *Handler) HandleChatMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	// Parse request body.
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now().UnixMilli()

	// --- Transaction: read session + save user message + update status ---
	tx, err := h.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		h.internalError(w, fmt.Errorf("begin tx: %w", err))
		return
	}
	defer tx.Rollback() // no-op if committed

	qtx := h.DB.WithTx(tx)

	// Load the session within the transaction.
	session, err := qtx.GetSessionByID(ctx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		h.internalError(w, fmt.Errorf("get session: %w", err))
		return
	}

	// Determine parent_id: use active_leaf_id if set.
	var parentID sql.NullString
	if session.ActiveLeafID.Valid {
		parentID = session.ActiveLeafID
	}

	// Save user message.
	userMsgID := newID()
	_, err = qtx.CreateMessage(ctx, db.CreateMessageParams{
		ID:        userMsgID,
		SessionID: sessionID,
		ParentID:  parentID,
		Role:      "user",
		Parts:     "[]",
		Content:   sql.NullString{String: req.Content, Valid: true},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		h.internalError(w, fmt.Errorf("save user message: %w", err))
		return
	}

	// Set session to working and update active_leaf to user message.
	if _, err := qtx.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           sessionID,
		Title:        session.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: sql.NullString{String: userMsgID, Valid: true},
		Status:       "working",
	}); err != nil {
		h.internalError(w, fmt.Errorf("update session status: %w", err))
		return
	}

	// Commit the transaction.
	if err := tx.Commit(); err != nil {
		h.internalError(w, fmt.Errorf("commit tx: %w", err))
		return
	}
	// --- End transaction ---

	// Build context: load full message history for this session.
	// Safe to use h.DB here (no transaction needed for reads after commit).
	allMessages, err := h.DB.ListMessagesBySession(ctx, sessionID)
	if err != nil {
		h.internalError(w, fmt.Errorf("load history: %w", err))
		return
	}

	// Convert DB messages to LLM messages.
	llmMessages := dbMessagesToLLM(allMessages)

	// Determine model: session agent_name-based lookup, fallback to config.
	model := h.Config.LLM.Model

	// Call LLM (outside transaction — may be slow).
	resp, err := h.LLM.ChatCompletion(ctx, &llm.ChatCompletionRequest{
		Model:    model,
		Messages: llmMessages,
	})
	if err != nil {
		h.saveLLMErrorAndRespond(ctx, w, sessionID, userMsgID, session, model, now, err)
		return
	}

	// Extract assistant content.
	var assistantContent string
	if len(resp.Choices) > 0 {
		assistantContent = resp.Choices[0].Message.Content
	}

	// Save assistant message and update session in a new transaction.
	assistantMsgID, err := h.saveAssistantMessage(ctx, sessionID, userMsgID, session, model, now, assistantContent, resp.Usage.TotalTokens)
	if err != nil {
		h.internalError(w, fmt.Errorf("save assistant response: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		SessionID: sessionID,
		MessageID: assistantMsgID,
		Role:      "assistant",
		Content:   assistantContent,
		Model:     model,
		Usage:     &resp.Usage,
	})
}

// saveAssistantMessage creates the assistant message and updates session status in a transaction.
// Returns the new message ID.
func (h *Handler) saveAssistantMessage(ctx context.Context, sessionID, userMsgID string, session db.Session, model string, now int64, content string, tokenCount int) (string, error) {
	tx, err := h.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := h.DB.WithTx(tx)

	assistantMsgID := newID()
	_, err = qtx.CreateMessage(ctx, db.CreateMessageParams{
		ID:         assistantMsgID,
		SessionID:  sessionID,
		ParentID:   sql.NullString{String: userMsgID, Valid: true},
		Role:       "assistant",
		Parts:      "[]",
		Content:    sql.NullString{String: content, Valid: true},
		Model:      sql.NullString{String: model, Valid: true},
		TokenCount: sql.NullInt64{Int64: int64(tokenCount), Valid: true},
		CreatedAt:  now + 1,
		UpdatedAt:  now + 1,
	})
	if err != nil {
		return "", fmt.Errorf("create assistant message: %w", err)
	}

	if _, err := qtx.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           sessionID,
		Title:        session.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: sql.NullString{String: assistantMsgID, Valid: true},
		Status:       "idle",
	}); err != nil {
		return "", fmt.Errorf("update session after chat: %w", err)
	}

	return assistantMsgID, tx.Commit()
}

// saveLLMErrorAndRespond saves an error message to the session and returns a 502 to the client.
func (h *Handler) saveLLMErrorAndRespond(ctx context.Context, w http.ResponseWriter, sessionID, userMsgID string, session db.Session, model string, now int64, llmErr error) {
	assistantMsgID := newID()
	errContent := "LLM error: " + llmErr.Error()

	// Attempt to save error message in the background — best effort.
	if err := func() error {
		tx, err := h.SQLDB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()

		qtx := h.DB.WithTx(tx)
		if _, err := qtx.CreateMessage(ctx, db.CreateMessageParams{
			ID:        assistantMsgID,
			SessionID: sessionID,
			ParentID:  sql.NullString{String: userMsgID, Valid: true},
			Role:      "assistant",
			Parts:     "[]",
			Content:   sql.NullString{String: errContent, Valid: true},
			CreatedAt: now + 1,
			UpdatedAt: now + 1,
		}); err != nil {
			return fmt.Errorf("create error message: %w", err)
		}
		if _, err := qtx.UpdateSession(ctx, db.UpdateSessionParams{
			ID:           sessionID,
			Title:        session.Title,
			AgentName:    session.AgentName,
			ActiveLeafID: sql.NullString{String: assistantMsgID, Valid: true},
			Status:       "idle",
		}); err != nil {
			return fmt.Errorf("update session after error: %w", err)
		}
		return tx.Commit()
	}(); err != nil {
		log.Printf("failed to save LLM error to session %s: %v", sessionID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(ChatResponse{
		SessionID: sessionID,
		MessageID: assistantMsgID,
		Role:      "assistant",
		Content:   errContent,
		Error:     llmErr.Error(),
	})
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

// HandleHealth responds with a simple health check.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Existing: OpenAI-compatible pass-through endpoint
// ---------------------------------------------------------------------------

// HandleChatCompletion accepts POST /v1/chat/completions with full OpenAI format,
// forwards to LLM client, returns response. No history persistence.
func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "messages array is required", http.StatusBadRequest)
		return
	}

	llmReq := &llm.ChatCompletionRequest{
		Model:      req.Model,
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
	}

	resp, err := h.LLM.ChatCompletion(r.Context(), llmReq)
	if err != nil {
		http.Error(w, "llm error: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toSessionView converts a db.Session into a JSON-safe SessionView.
func toSessionView(s db.Session, msgs []MessageView) SessionView {
	return SessionView{
		ID:           s.ID,
		AgentName:    s.AgentName,
		Title:        s.Title,
		Status:       s.Status,
		MessageCount: s.MessageCount,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		Messages:     msgs,
	}
}

// toMessageViews converts DB messages to API message views.
func toMessageViews(msgs []db.Message) []MessageView {
	views := make([]MessageView, 0, len(msgs))
	for _, m := range msgs {
		var content string
		if m.Content.Valid {
			content = m.Content.String
		}
		var agentName string
		if m.AgentName.Valid {
			agentName = m.AgentName.String
		}
		views = append(views, MessageView{
			ID:        m.ID,
			Role:      m.Role,
			Content:   content,
			AgentName: agentName,
			CreatedAt: m.CreatedAt,
		})
	}
	return views
}

// dbMessagesToLLM converts a slice of DB messages to LLM API messages.
// Only user and assistant messages are included; tool messages are skipped
// for the linear history demo.
func dbMessagesToLLM(msgs []db.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		var content string
		if m.Content.Valid {
			content = m.Content.String
		}
		out = append(out, llm.Message{
			Role:    m.Role,
			Content: content,
		})
	}
	return out
}
