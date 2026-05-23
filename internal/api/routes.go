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
	mux.HandleFunc("DELETE /session/{id}", h.HandleDeleteSession)

	// Chat
	mux.HandleFunc("POST /session/{id}/message", h.HandleChatMessage)

	// Legacy: OpenAI-compatible pass-through
	mux.HandleFunc("POST /v1/chat/completions", h.HandleChatCompletion)
}
