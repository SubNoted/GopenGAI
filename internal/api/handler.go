package api

import (
	"encoding/json"
	"net/http"

	"gopengai/internal/llm"
)

// Handler holds the dependencies for HTTP request handlers.
type Handler struct {
	LLM *llm.Client
}

// ChatRequest is the request body from a client hitting the chat endpoint.
type ChatRequest struct {
	Model      string               `json:"model"`
	Messages   []llm.Message        `json:"messages"`
	Tools      []llm.ToolDefinition `json:"tools,omitempty"`
	ToolChoice json.RawMessage      `json:"tool_choice,omitempty"`
}

// HandleChatCompletion accepts POST /v1/chat/completions, forwards to LLM client, returns response.
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

	// Pass tools and tool_choice from the incoming request.
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

// HandleHealth responds with a simple health check.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
