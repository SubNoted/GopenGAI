package api

import "net/http"

// RegisterRoutes wires all handler methods to the HTTP serve mux.
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/v1/chat/completions", h.HandleChatCompletion)
}
