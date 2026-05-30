package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("https://api.test/v1", "sk-test", "gpt-4")
	if c.BaseURL != "https://api.test/v1" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.APIKey != "sk-test" {
		t.Errorf("APIKey = %q", c.APIKey)
	}
	if c.Model != "gpt-4" {
		t.Errorf("Model = %q", c.Model)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
}

func TestChatCompletion(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer sk-test" {
				t.Errorf("expected Authorization header")
			}

			// Verify request body structure.
			var req ChatCompletionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Model != "gpt-4" {
				t.Errorf("req.Model = %q, want %q", req.Model, "gpt-4")
			}

			// Return successful response.
			resp := ChatCompletionResponse{
				ID:      "chatcmpl-123",
				Object:  "chat.completion",
				Model:   "gpt-4",
				Created: 1700000000,
				Choices: []Choice{
					{
						Index: 0,
						Message: MessageResponse{
							Role:    "assistant",
							Content: "Hello! How can I help?",
						},
						FinishReason: "stop",
					},
				},
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{
				{Role: "user", Content: "Hello"},
			},
		}
		resp, err := c.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}
		if resp.Choices[0].Message.Content != "Hello! How can I help?" {
			t.Errorf("Content = %q", resp.Choices[0].Message.Content)
		}
		if resp.Usage.TotalTokens != 15 {
			t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
		}
	})

	t.Run("model from client when request model is empty", func(t *testing.T) {
		var receivedModel string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req ChatCompletionRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedModel = req.Model

			resp := ChatCompletionResponse{
				Choices: []Choice{{
					Index:   0,
					Message: MessageResponse{Role: "assistant", Content: "ok"},
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "client-default-model")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			// Model intentionally left empty.
		}
		_, err := c.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}
		if receivedModel != "client-default-model" {
			t.Errorf("received Model = %q, want %q", receivedModel, "client-default-model")
		}
	})

	t.Run("API error response (non-200)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			// OpenAI-compatible error format: APIError fields at root level.
			json.NewEncoder(w).Encode(APIError{
				Message: "Rate limit exceeded",
				Type:    "rate_limit_error",
				Code:    "rate_limited",
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		}
		_, err := c.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatal("expected error for non-200 response")
		}

		llmErr, ok := err.(*LLMError)
		if !ok {
			t.Fatalf("expected *LLMError, got %T", err)
		}
		if llmErr.StatusCode != http.StatusTooManyRequests {
			t.Errorf("StatusCode = %d, want 429", llmErr.StatusCode)
		}
		if llmErr.Message != "Rate limit exceeded" {
			t.Errorf("Message = %q", llmErr.Message)
		}
		if llmErr.APIError == nil {
			t.Fatal("APIError should not be nil")
		}
		if llmErr.APIError.Type != "rate_limit_error" {
			t.Errorf("APIError.Type = %q", llmErr.APIError.Type)
		}
	})

	t.Run("API error without JSON body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("plain text error"))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		}
		_, err := c.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatal("expected error")
		}
		llmErr := err.(*LLMError)
		if llmErr.Message != "plain text error" {
			t.Errorf("Message = %q, want %q", llmErr.Message, "plain text error")
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		c := NewClient("http://127.0.0.1:19999", "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		}
		_, err := c.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatal("expected error for connection refused")
		}
		llmErr, ok := err.(*LLMError)
		if !ok {
			t.Fatalf("expected *LLMError, got %T", err)
		}
		if llmErr.StatusCode != 0 {
			t.Errorf("StatusCode = %d, want 0", llmErr.StatusCode)
		}
	})

	t.Run("tool calling response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := ChatCompletionResponse{
				ID:    "chatcmpl-tool-1",
				Model: "gpt-4",
				Choices: []Choice{{
					Index: 0,
					Message: MessageResponse{
						Role:    "assistant",
						Content: "",
						ToolCalls: []ToolCall{
							{
								ID:   "call_abc",
								Type: "function",
								Function: FunctionCall{
									Name:      "web_fetch",
									Arguments: `{"url":"https://example.com"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "fetch example.com"}},
			Tools: []ToolDefinition{{
				Type: "function",
				Function: ToolFunction{
					Name:        "web_fetch",
					Description: "Fetches a URL",
				},
			}},
		}
		resp, err := c.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}
		if resp.Choices[0].FinishReason != "tool_calls" {
			t.Errorf("FinishReason = %q, want %q", resp.Choices[0].FinishReason, "tool_calls")
		}
		toolCalls := resp.Choices[0].Message.ToolCalls
		if len(toolCalls) != 1 {
			t.Fatalf("len(ToolCalls) = %d, want 1", len(toolCalls))
		}
		if toolCalls[0].Function.Name != "web_fetch" {
			t.Errorf("ToolCall name = %q", toolCalls[0].Function.Name)
		}
	})

	t.Run("invalid JSON response body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{bad json`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		}
		_, err := c.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Slow response — but context is already cancelled.
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel

		c := NewClient(srv.URL, "sk-test", "gpt-4")
		req := &ChatCompletionRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		}
		_, err := c.ChatCompletion(ctx, req)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

func TestLLMError_Error(t *testing.T) {
	t.Run("with APIError details", func(t *testing.T) {
		e := &LLMError{
			StatusCode: 429,
			Message:    "Rate limited",
			APIError: &APIError{
				Message: "Rate limit exceeded",
				Type:    "rate_limit_error",
				Code:    "rate_limited",
			},
		}
		errStr := e.Error()
		if errStr == "" {
			t.Error("Error() returned empty string")
		}
	})

	t.Run("without APIError", func(t *testing.T) {
		e := &LLMError{
			StatusCode: 500,
			Message:    "server error",
		}
		errStr := e.Error()
		if errStr == "" {
			t.Error("Error() returned empty string")
		}
	})
}
