# GoPengAI — MVP TODO (Minimal Viable Agent)

> **Goal:** A working HTTP server that accepts a user message, sends it to OpenAI's `/v1/chat/completions` endpoint, and returns the response. No history, no persistence, no tools, no SSE, no CLI.
>
> **What MVP means:** You can `curl` a question and get an AI answer back. That's it.
>
> **Tech Stack:** Go 1.21+, `net/http` (stdlib), OpenAI-compatible API
>
> **Total files to implement:** 6 files, ~200 lines of Go

---

## What We're NOT Building (Yet)

| Feature | Why Skip |
|---------|----------|
| SQLite / sqlc / Goose | No history to persist |
| Agent registry / .md loader | Single hardcoded agent |
| Tools (web_fetch, memory, delegate) | MVP = pure Q&A |
| SSE event streaming | Synchronous request/response |
| History tree / branches | No conversation memory |
| CLI client | curl is enough |
| Streaming LLM output | Simple non-streaming first |

---

## Files to Implement (in order)

```
internal/config/config.go   → Config struct + JSON loader
internal/llm/types.go       → OpenAI request/response types
internal/llm/client.go      → HTTP client for OpenAI API
internal/api/handler.go     → POST /v1/chat/completions handler
internal/api/routes.go      → Route registration
cmd/api/main.go             → Wire everything + start server
```

---

## Step 1: Config (`internal/config/config.go`)

**Goal:** Load `gopengai.json` into a Go struct.

```go
package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server ServerConfig `json:"server"`
	LLM    LLMConfig    `json:"llm"`
}

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type LLMConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// Defaults
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "gpt-4o-mini"
	}
	return &cfg, nil
}
```

**Test:** Write a test that loads `gopengai.json.example` and checks defaults are applied.

---

## Step 2: LLM Types (`internal/llm/types.go`)

**Goal:** Structs matching OpenAI `/v1/chat/completions` request/response format.

```go
package llm

// Request types

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response types

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Error   *APIError `json:"error,omitempty"`
}

type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}
```

---

## Step 3: LLM Client (`internal/llm/client.go`)

**Goal:** Send a chat completion request to OpenAI and return the response.

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (*ChatCompletionResponse, error) {
	req := ChatCompletionRequest{
		Model:    c.Model,
		Messages: messages,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var completion ChatCompletionResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &completion, nil
}
```

**Test:** Write a test using `httptest.NewServer` that mocks OpenAI, sends a message, asserts response.

---

## Step 4: API Handler (`internal/api/handler.go`)

**Goal:** Accept `POST /v1/chat/completions`, forward to LLM client, return response.

```go
package api

import (
	"encoding/json"
	"net/http"

	"gopengai/internal/llm"
)

type Handler struct {
	LLM *llm.Client
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
}

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

	resp, err := h.LLM.ChatCompletion(r.Context(), req.Messages)
	if err != nil {
		http.Error(w, "llm error: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

---

## Step 5: Routes (`internal/api/routes.go`)

**Goal:** Wire routes to handler methods.

```go
package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/v1/chat/completions", h.HandleChatCompletion)
}
```

---

## Step 6: Main Entrypoint (`cmd/api/main.go`)

**Goal:** Load config, create LLM client, create handler, start HTTP server.

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"gopengai/internal/api"
	"gopengai/internal/config"
	"gopengai/internal/llm"
)

func main() {
	cfgPath := "gopengai.json"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	client := llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	handler := &api.Handler{LLM: client}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, handler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("GoPengAI MVP listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

---

## Step 7: Verify Build

```bash
go build ./cmd/api/
go vet ./...
```

---

## Step 8: Manual Test

```bash
# Terminal 1: Start server
./api

# Terminal 2: Test health
curl http://localhost:8080/health
# Expected: {"status":"ok"}

# Terminal 3: Test chat
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "What is Go?"}
    ]
  }'
# Expected: OpenAI-style response with AI answer
```

---

## Summary

| # | File | Lines | Purpose |
|---|------|-------|---------|
| 1 | `internal/config/config.go` | ~45 | Load JSON config with defaults |
| 2 | `internal/llm/types.go` | ~40 | OpenAI request/response structs |
| 3 | `internal/llm/client.go` | ~60 | HTTP client → OpenAI API |
| 4 | `internal/api/handler.go` | ~50 | POST /v1/chat/completions handler |
| 5 | `internal/api/routes.go` | ~10 | Route registration |
| 6 | `cmd/api/main.go` | ~35 | Wire + start server |
| **Total** | | **~240** | **Working AI agent** |

---

## After MVP Works — Next Steps (for full TODO.md)

1. **History:** Add SQLite + messages table → conversation memory
2. **Streaming:** Add SSE → real-time token delivery
3. **Agent Loader:** Parse `.md` files → configurable system prompts
4. **Tools:** Add tool interface → web_fetch, memory, delegate
5. **CLI:** Add Cobra CLI → terminal chat client
