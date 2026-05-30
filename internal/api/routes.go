package api

import "net/http"

// RegisterRoutes wires all handler methods to the HTTP serve mux.
// Uses Go 1.22+ routing patterns (method prefix + path parameters).
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	// Health
	mux.HandleFunc("GET /health", h.HandleHealth)

	// Session CRUD
	mux.HandleFunc("POST /session", h.HandleCreateSession)
	mux.HandleFunc("GET /session", h.HandleListSessions)
	mux.HandleFunc("GET /session/{id}", h.HandleGetSession)
	mux.HandleFunc("PATCH /session/{id}", h.HandleUpdateSession)
	mux.HandleFunc("DELETE /session/{id}", h.HandleDeleteSession)

	// Session messages (active branch)
	mux.HandleFunc("GET /session/{id}/messages", h.HandleGetSessionMessages)

	// Session branches (leaf listing)
	mux.HandleFunc("GET /session/{id}/branches", h.HandleListBranches)

	// Session fork
	mux.HandleFunc("POST /session/{id}/fork", h.HandleForkSession)

	// Session branch selection
	mux.HandleFunc("PUT /session/{id}/branch", h.HandleSelectBranch)

	// Session abort
	mux.HandleFunc("POST /session/{id}/abort", h.HandleAbortSession)

	// Chat
	mux.HandleFunc("POST /session/{id}/message", h.HandleChatMessage)

	// SSE streaming (real-time agent events)
	mux.HandleFunc("GET /events", h.HandleGlobalSSE)
	mux.HandleFunc("GET /session/{id}/events", h.HandleSessionSSE)

	// Message edit (branch creation)
	mux.HandleFunc("PATCH /messages/{id}", h.HandleEditMessage)

	// Agent listing
	mux.HandleFunc("GET /agents", h.HandleListAgents)
	mux.HandleFunc("GET /agents/{name}", h.HandleGetAgent)

	// Memory
	mux.HandleFunc("GET /memory", h.HandleListMemory)
	mux.HandleFunc("GET /memory/{key}", h.HandleGetMemory)

	// Models
	mux.HandleFunc("GET /v1/models", h.HandleListModels)

	// Legacy: OpenAI-compatible pass-through
	mux.HandleFunc("POST /v1/chat/completions", h.HandleChatCompletion)
}
