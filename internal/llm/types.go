package llm

import "encoding/json"

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// ChatCompletionRequest matches the OpenAI /v1/chat/completions request format.
type ChatCompletionRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice json.RawMessage  `json:"tool_choice,omitempty"` // "auto" | "none" | {"type":"function","function":{"name":"..."}}
}

// Message represents a single message in a chat conversation.
// For tool-calling support, Role may be "system", "user", "assistant", or "tool".
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolDefinition describes a tool that the LLM may call.
type ToolDefinition struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes the function signature of a tool.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// ChatCompletionResponse matches the OpenAI /v1/chat/completions response format.
type ChatCompletionResponse struct {
	ID      string    `json:"id"`
	Object  string    `json:"object,omitempty"`
	Created int64     `json:"created,omitempty"`
	Model   string    `json:"model,omitempty"`
	Choices []Choice  `json:"choices"`
	Usage   Usage     `json:"usage"`
	Error   *APIError `json:"error,omitempty"`
}

// Choice represents a single completion choice from the LLM.
type Choice struct {
	Index        int             `json:"index"`
	Message      MessageResponse `json:"message"`
	FinishReason string          `json:"finish_reason,omitempty"` // "stop" | "tool_calls" | "length" | ...
}

// MessageResponse is the assistant message returned by the LLM.
// It can contain either Content (text) or ToolCalls (function invocations).
type MessageResponse struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a function call requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the name and arguments for a tool invocation.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string to be unmarshalled by the tool
}

// Usage contains token usage information from the LLM.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// APIError represents an error from the OpenAI API.
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// ---------------------------------------------------------------------------
// Streaming types (used by stream.go)
// ---------------------------------------------------------------------------

// StreamCompletionResponse is a single SSE event from a streaming completion.
type StreamCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"` // usually only in final event
}

// StreamChoice represents a single streaming choice delta.
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// StreamDelta is the incremental content within a streaming chunk.
type StreamDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
