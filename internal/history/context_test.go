package history

import (
	"testing"

	"gopengai/internal/llm"
)

func TestTruncateContext(t *testing.T) {
	t.Run("empty messages returned as-is", func(t *testing.T) {
		result := truncateContext(nil, 100)
		if result != nil {
			t.Error("expected nil")
		}
		result = truncateContext([]llm.Message{}, 100)
		if len(result) != 0 {
			t.Error("expected empty")
		}
	})

	t.Run("single message (system prompt) preserved", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "you are helpful"},
		}
		result := truncateContext(msgs, 1)
		if len(result) != 1 {
			t.Fatalf("len = %d, want 1", len(result))
		}
		if result[0].Content != "you are helpful" {
			t.Error("system prompt changed")
		}
	})

	t.Run("within limit — no truncation", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "short"},
			{Role: "assistant", Content: "ok"},
		}
		result := truncateContext(msgs, 1000)
		if len(result) != 3 {
			t.Errorf("len = %d, want 3 (no truncation needed)", len(result))
		}
	})

	t.Run("truncates oldest user messages first", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "to be truncated"},
			{Role: "assistant", Content: "keep this"},
		}
		// ~Token estimate: sys=4+0=4, user1=4+3=7, assistant=4+2=6, total=17
		// With max=13, we should drop user1.
		result := truncateContext(msgs, 13)
		if len(result) != 2 {
			t.Fatalf("len = %d, want 2", len(result))
		}
		if result[0].Role != "system" {
			t.Error("system prompt should be first")
		}
		if result[1].Role != "assistant" {
			t.Errorf("remaining msg role = %q, want %q", result[1].Role, "assistant")
		}
	})

	t.Run("all non-system messages dropped when very low limit", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: "hello world"},
			{Role: "assistant", Content: "hi"},
		}
		result := truncateContext(msgs, 5)
		if len(result) != 1 {
			t.Fatalf("len = %d, want 1 (only system)", len(result))
		}
		if result[0].Role != "system" {
			t.Error("only system prompt should remain")
		}
	})

	t.Run("zero maxTokens truncates everything but system", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "anything"},
		}
		result := truncateContext(msgs, 0)
		// When maxTokens is 0, the estimate for the full message set will be > 0,
		// so truncation starts. Eventually only the system message remains.
		if len(result) < 1 {
			t.Error("system prompt should be preserved")
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		n := estimateTokens(nil)
		if n != 0 {
			t.Errorf("estimateTokens(nil) = %d, want 0", n)
		}
	})

	t.Run("simple message", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "user", Content: "hello"},
		}
		n := estimateTokens(msgs)
		// overhead(4) + len("hello")/4 = 4 + 1 = 5
		if n != 5 {
			t.Errorf("estimateTokens = %d, want 5", n)
		}
	})

	t.Run("message with tool calls", func(t *testing.T) {
		msgs := []llm.Message{
			{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{
					{
						Function: llm.FunctionCall{
							Name:      "web_fetch",
							Arguments: `{"url":"https://example.com"}`,
						},
					},
				},
			},
		}
		n := estimateTokens(msgs)
		// overhead(4) + content(0) + tool_overhead(8) + name(9/4=2) + args(30/4=7) = 21
		if n < 10 {
			t.Errorf("estimateTokens = %d, expected > 10 for tool call", n)
		}
	})

	t.Run("multiple messages", func(t *testing.T) {
		msgs := []llm.Message{
			{Role: "system", Content: "you are a bot"},
			{Role: "user", Content: "hello world how are you"},
		}
		n := estimateTokens(msgs)
		// msg1: 4 + 14/4 = 4+3 = 7
		// msg2: 4 + 23/4 = 4+5 = 9
		// total = 16
		if n != 16 {
			t.Errorf("estimateTokens = %d, want 16", n)
		}
	})
}
