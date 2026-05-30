package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"gopengai/internal/agent"
	"gopengai/internal/config"
	"gopengai/internal/db"
	"gopengai/internal/history"
	"gopengai/internal/llm"
)

// Handler holds the dependencies for HTTP request handlers.
type Handler struct {
	LLM      *llm.Client
	DB       *db.Queries
	SQLDB    *sql.DB
	Config   *config.Config
	Engine   *agent.Engine
	EventBus *EventBus
	History  *history.Repository

	// Wg tracks active async engine goroutines. Callers should Wg.Add(1)
	// before spawning a goroutine and Wg.Done() when it completes. Used for
	// graceful shutdown: main waits on Wg before closing the database.
	Wg *sync.WaitGroup
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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
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
	encodeJSON(w, toSessionView(session, nil))
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
	encodeJSON(w, views)
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
	encodeJSON(w, toSessionView(session, toMessageViews(messages)))
}

// HandleDeleteSession handles DELETE /session/{id}.
func (h *Handler) HandleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	// Wrap delete in a transaction to ensure atomicity: if session deletion
	// fails after message deletion, the transaction rolls back both.
	tx, err := h.SQLDB.BeginTx(r.Context(), nil)
	if err != nil {
		h.internalError(w, fmt.Errorf("begin delete tx: %w", err))
		return
	}
	defer tx.Rollback()

	qtx := h.DB.WithTx(tx)

	if err := qtx.DeleteSessionMessages(r.Context(), id); err != nil {
		h.internalError(w, fmt.Errorf("delete messages: %w", err))
		return
	}
	if err := qtx.DeleteSession(r.Context(), id); err != nil {
		h.internalError(w, fmt.Errorf("delete session: %w", err))
		return
	}

	if err := tx.Commit(); err != nil {
		h.internalError(w, fmt.Errorf("commit delete tx: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Chat endpoint (async, delegated to agent engine)
// ---------------------------------------------------------------------------

// maxContentLength is the maximum allowed size for user message content.
const maxContentLength = 100 * 1024 // 100 KB

// maxRequestBodySize is the maximum accepted HTTP request body size (1 MB).
// Applied via http.MaxBytesReader to all endpoints that decode JSON bodies.
const maxRequestBodySize = 1 << 20 // 1 MB

// HandleChatMessage handles POST /session/{id}/message.
//
// This endpoint is async: it validates the request, updates the session status
// to "working", returns 202 Accepted immediately, and spawns a goroutine that
// delegates to engine.Process(). The engine handles saving the user message,
// building LLM context, calling the LLM with the tool-calling loop, saving
// responses, publishing SSE events, and resetting the DB status to "idle".
//
// Clients should subscribe to GET /session/{id}/events (SSE) to receive
// real-time progress events (message.part.added, message.complete,
// message.error, session.status, etc.).
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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if len(req.Content) > maxContentLength {
		http.Error(w, "content exceeds maximum size (100KB)", http.StatusRequestEntityTooLarge)
		return
	}

	ctx := r.Context()

	// Read session to get agent_name and validate existence.
	session, err := h.DB.GetSessionByID(ctx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		h.internalError(w, fmt.Errorf("get session: %w", err))
		return
	}

	agentName := session.AgentName
	if agentName == "" {
		agentName = h.Config.DefaultAgent
	}

	// Atomically try to claim the session (idle → working). Two concurrent
	// requests for the same session cannot both pass this check — the DB-level
	// WHERE status='idle' clause guarantees mutual exclusion.
	sqlResult, err := h.SQLDB.ExecContext(ctx,
		"UPDATE sessions SET status = 'working' WHERE id = ? AND status = 'idle'", sessionID)
	if err != nil {
		h.internalError(w, fmt.Errorf("claim session: %w", err))
		return
	}
	rows, _ := sqlResult.RowsAffected()
	if rows == 0 {
		http.Error(w, "session is already processing a message", http.StatusConflict)
		return
	}

	// Return 202 Accepted immediately — the client must use SSE to
	// receive the actual response.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	encodeJSON(w, map[string]string{
		"session_id": sessionID,
		"status":     "accepted",
	})

	// Spawn goroutine for async processing with panic recovery, timeout,
	// and WaitGroup tracking for graceful shutdown.
	h.Wg.Add(1)
	go func() {
		defer h.Wg.Done()

		// Panic recovery: if the engine or DB code panics, catch it,
		// log the stack trace, and reset session status to "idle" so
		// the session is not permanently stuck in "working".
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC in chat goroutine session %s: %v\n%s",
					sessionID, rec, debug.Stack())
			}
			// Always reset the DB status to "idle", regardless of panic
			// or normal completion. Use a fresh context with timeout so
			// we can still update even if the engine context is cancelled.
			resetCtx, resetCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer resetCancel()
			// Re-read session so we don't overwrite fields (active_leaf)
			// that the engine may have updated.
			if s, serr := h.DB.GetSessionByID(resetCtx, sessionID); serr == nil {
				_, _ = h.DB.UpdateSession(resetCtx, db.UpdateSessionParams{
					ID:           sessionID,
					Title:        s.Title,
					AgentName:    s.AgentName,
					ActiveLeafID: s.ActiveLeafID,
					Status:       "idle",
				})
			} else {
				log.Printf("failed to load session %s for status reset: %v", sessionID, serr)
			}
		}()

		// Apply a 5-minute timeout to prevent permanently hanging
		// goroutines (e.g., LLM TCP timeout, stuck tool fetch).
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer bgCancel()
		if err := h.Engine.Process(bgCtx, sessionID, req.Content, agentName); err != nil {
			log.Printf("engine process error for session %s: %v", sessionID, err)
		}
	}()
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

// HandleHealth responds with a simple health check.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Existing: OpenAI-compatible pass-through endpoint
// ---------------------------------------------------------------------------

// HandleChatCompletion accepts POST /v1/chat/completions with full OpenAI format,
// forwards to LLM client, returns response. No history persistence.
func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
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
		log.Printf("LLM error in HandleChatCompletion: %v", err)
		http.Error(w, "llm error", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, resp)
}

// ---------------------------------------------------------------------------
// Session update
// ---------------------------------------------------------------------------

// HandleUpdateSession handles PATCH /session/{id}.
// Allows updating the session title.
func (h *Handler) HandleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	var req struct {
		Title string `json:"title,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
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

	updated, err := h.DB.UpdateSession(r.Context(), db.UpdateSessionParams{
		ID:           id,
		Title:        req.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: session.ActiveLeafID,
		Status:       session.Status,
	})
	if err != nil {
		h.internalError(w, fmt.Errorf("update session: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, toSessionView(updated, nil))
}

// ---------------------------------------------------------------------------
// Active branch messages
// ---------------------------------------------------------------------------

// HandleGetSessionMessages handles GET /session/{id}/messages.
// Returns the active branch messages (root-to-leaf path using recursive CTE).
func (h *Handler) HandleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	messages, err := h.History.GetActiveBranch(r.Context(), id)
	if err != nil {
		h.internalError(w, fmt.Errorf("get active branch: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, toMessageViews(messages))
}

// ---------------------------------------------------------------------------
// Branches (leaves listing)
// ---------------------------------------------------------------------------

// HandleListBranches handles GET /session/{id}/branches.
// Returns all leaf messages in the session (each leaf = one branch tip).
func (h *Handler) HandleListBranches(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	leaves, err := h.History.GetAllLeaves(r.Context(), id)
	if err != nil {
		h.internalError(w, fmt.Errorf("list branches: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, toMessageViews(leaves))
}

// ---------------------------------------------------------------------------
// Fork session
// ---------------------------------------------------------------------------

// ForkRequest is the request body for POST /session/{id}/fork.
type ForkRequest struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	Title     string `json:"title,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

// HandleForkSession handles POST /session/{id}/fork.
// Forks the session at the given message and creates a new session.
func (h *Handler) HandleForkSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	var req ForkRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MessageID == "" {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	newSessionID, err := h.History.ForkSession(r.Context(), history.ForkSessionParams{
		OriginalSessionID: sessionID,
		AgentName:         req.AgentName,
		Title:             req.Title,
		FromMessageID:     req.MessageID,
		NewContent:        req.Content,
	})
	if err != nil {
		h.internalError(w, fmt.Errorf("fork session: %w", err))
		return
	}

	newSession, err := h.DB.GetSessionByID(r.Context(), newSessionID)
	if err != nil {
		h.internalError(w, fmt.Errorf("get forked session: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	encodeJSON(w, toSessionView(newSession, nil))
}

// ---------------------------------------------------------------------------
// Select branch
// ---------------------------------------------------------------------------

// SelectBranchRequest is the request body for PUT /session/{id}/branch.
type SelectBranchRequest struct {
	LeafID string `json:"leaf_id"`
}

// HandleSelectBranch handles PUT /session/{id}/branch.
// Sets the active branch by selecting a leaf node.
func (h *Handler) HandleSelectBranch(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	var req SelectBranchRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.LeafID == "" {
		http.Error(w, "leaf_id is required", http.StatusBadRequest)
		return
	}

	if err := h.History.SelectLeaf(r.Context(), sessionID, req.LeafID); err != nil {
		http.Error(w, "invalid branch selection", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Edit message (branch creation)
// ---------------------------------------------------------------------------

// EditMessageRequest is the request body for PATCH /messages/{id}.
type EditMessageRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Role      string `json:"role"`
}

// HandleEditMessage handles PATCH /messages/{id}.
// Creates a new branch by editing a message (new sibling with same parent).
func (h *Handler) HandleEditMessage(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		http.Error(w, "missing message id", http.StatusBadRequest)
		return
	}

	var req EditMessageRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}

	newMsgID, err := h.History.EditMessage(r.Context(), history.EditMessageParams{
		SessionID: req.SessionID,
		TargetID:  msgID,
		Content:   req.Content,
		Role:      req.Role,
	})
	if err != nil {
		http.Error(w, "edit failed", http.StatusBadRequest)
		return
	}

	newMsg, err := h.DB.GetMessage(r.Context(), newMsgID)
	if err != nil {
		h.internalError(w, fmt.Errorf("get edited message: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	encodeJSON(w, toMessageViews([]db.Message{newMsg})[0])
}

// ---------------------------------------------------------------------------
// Agent listing
// ---------------------------------------------------------------------------

// AgentView is a JSON-friendly agent representation for API responses.
type AgentView struct {
	Name        string   `json:"name"`
	Model       string   `json:"model,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	ParentAgent string   `json:"parent_agent,omitempty"`
	Description string   `json:"description,omitempty"`
	Mode        string   `json:"mode,omitempty"`
}

// HandleListAgents handles GET /agents.
// Returns all registered agents from the in-memory registry.
func (h *Handler) HandleListAgents(w http.ResponseWriter, r *http.Request) {
	agents := h.Engine.Agents.List()
	views := make([]AgentView, 0, len(agents))
	for _, a := range agents {
		views = append(views, toAgentView(a))
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, views)
}

// HandleGetAgent handles GET /agents/{name}.
// Returns details for a specific agent.
func (h *Handler) HandleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing agent name", http.StatusBadRequest)
		return
	}

	agent, err := h.Engine.Agents.Get(name)
	if err != nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, toAgentView(*agent))
}

// toAgentView converts a agent.Agent into a JSON-safe AgentView.
func toAgentView(a agent.Agent) AgentView {
	return AgentView{
		Name:        a.Name,
		Model:       a.Model,
		Tools:       a.Tools,
		ParentAgent: a.ParentAgent,
		Description: a.Description,
		Mode:        a.Mode,
	}
}

// ---------------------------------------------------------------------------
// Memory endpoints
// ---------------------------------------------------------------------------

// MemoryView is a JSON-friendly memory representation.
type MemoryView struct {
	ID        string `json:"id"`
	AgentName string `json:"agent_name"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Category  string `json:"category,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// HandleListMemory handles GET /memory?agent=NAME.
// Lists all memory facts for the given agent.
func (h *Handler) HandleListMemory(w http.ResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		agentName = h.Config.DefaultAgent
	}

	facts, err := h.DB.ListMemoryByAgent(r.Context(), agentName)
	if err != nil {
		h.internalError(w, fmt.Errorf("list memory: %w", err))
		return
	}

	views := make([]MemoryView, 0, len(facts))
	for _, f := range facts {
		views = append(views, toMemoryView(f))
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, views)
}

// HandleGetMemory handles GET /memory/{key}?agent=NAME.
// Returns a specific memory fact.
func (h *Handler) HandleGetMemory(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "missing memory key", http.StatusBadRequest)
		return
	}

	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		agentName = h.Config.DefaultAgent
	}

	fact, err := h.DB.GetMemory(r.Context(), db.GetMemoryParams{
		AgentName: agentName,
		Key:       key,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "memory not found", http.StatusNotFound)
			return
		}
		h.internalError(w, fmt.Errorf("get memory: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, toMemoryView(fact))
}

// toMemoryView converts a db.Memory into a JSON-safe MemoryView.
func toMemoryView(m db.Memory) MemoryView {
	v := MemoryView{
		ID:        m.ID,
		AgentName: m.AgentName,
		Key:       m.Key,
		Value:     m.Value,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
	if m.Category.Valid {
		v.Category = m.Category.String
	}
	return v
}

// ---------------------------------------------------------------------------
// Abort
// ---------------------------------------------------------------------------

// HandleAbortSession handles POST /session/{id}/abort.
// Cancels any running agent process for the session.
func (h *Handler) HandleAbortSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	if err := h.Engine.Abort(sessionID); err != nil {
		// No active process is not an error for the client — treat as no-op success.
		log.Printf("abort session %s: %v", sessionID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, map[string]string{"status": "aborted"})
}

// ---------------------------------------------------------------------------
// Models listing
// ---------------------------------------------------------------------------

// ModelView is a JSON-friendly model representation (OpenAI-compatible).
type ModelView struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// HandleListModels handles GET /v1/models.
// Lists agents as models for OpenAI-compatible tooling.
func (h *Handler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	agents := h.Engine.Agents.List()
	models := make([]ModelView, 0, len(agents)+1)

	// Add the default config model first.
	models = append(models, ModelView{
		ID:      h.Config.LLM.Model,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "gopengai",
	})

	// Add agent-specific models.
	for _, a := range agents {
		modelID := a.Model
		if modelID == "" {
			modelID = h.Config.LLM.Model
		}
		models = append(models, ModelView{
			ID:      modelID,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "agent:" + a.Name,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSON(w, map[string]interface{}{
		"object": "list",
		"data":   models,
	})
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

// encodeJSON writes v as JSON to w and logs any write error for debugging.
// Use in place of json.NewEncoder(w).Encode(v) to prevent silently dropped
// errors when the client disconnects mid-response.
func encodeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
