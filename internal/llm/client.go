package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gopengai/internal/config"
)

// Client is an HTTP client for OpenAI-compatible /v1/chat/completions endpoints.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewClient creates a new LLM client from individual parameters.
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{},
	}
}

// NewClientFromConfig creates a new LLM client from an LLMConfig struct.
func NewClientFromConfig(cfg config.LLMConfig) *Client {
	return NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
}

// ChatCompletion sends a chat completion request to the LLM provider and returns the response.
// The request supports messages, tool definitions, and tool_choice.
func (c *Client) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Fill in model from client if not set on the request.
	if req.Model == "" {
		req.Model = c.Model
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
		return nil, &LLMError{
			StatusCode: 0,
			Message:    fmt.Sprintf("http request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &LLMError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("read response body: %v", err),
		}
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse the API error response body.
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return nil, &LLMError{
				StatusCode: resp.StatusCode,
				Message:    apiErr.Message,
				APIError:   &apiErr,
			}
		}
		return nil, &LLMError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	var completion ChatCompletionResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &completion, nil
}

// ---------------------------------------------------------------------------
// Structured error type
// ---------------------------------------------------------------------------

// LLMError represents a structured error from the LLM API call.
type LLMError struct {
	StatusCode int
	Message    string
	APIError   *APIError `json:"api_error,omitempty"`
}

func (e *LLMError) Error() string {
	if e.APIError != nil {
		return fmt.Sprintf("llm api error (status %d): %s (type=%s code=%s)", e.StatusCode, e.APIError.Message, e.APIError.Type, e.APIError.Code)
	}
	return fmt.Sprintf("llm error (status %d): %s", e.StatusCode, e.Message)
}
