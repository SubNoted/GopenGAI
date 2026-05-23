package history

import (
	"context"
	"fmt"

	"gopengai/internal/llm"
)

// approximateTokensPerChar is a rough heuristic for token counting.
// ~4 characters per token is typical for English text.
const approximateTokensPerChar = 4

// BuildContext loads the active branch for a session, prepends the system
// prompt, converts everything to []llm.Message, and truncates if the
// estimated token count exceeds maxTokens.
//
// Parameters:
//   - ctx: context for cancellation
//   - sessionID: the session to build context for
//   - systemPrompt: the agent's system prompt (prepended as a system message)
//   - maxTokens: soft limit for total context tokens (<= 0 = no limit)
//
// Returns the formatted message list ready for LLM consumption.
func (r *Repository) BuildContext(ctx context.Context, sessionID, systemPrompt string, maxTokens int) ([]llm.Message, error) {
	// Load the active branch.
	branch, err := r.GetActiveBranch(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("build context — get active branch: %w", err)
	}

	// Convert to agent messages.
	agentMsgs := ToAgentMessages(branch)

	// Build the message list with system prompt first.
	result := make([]llm.Message, 0, 1+len(agentMsgs))
	result = append(result, llm.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	for _, am := range agentMsgs {
		m := llm.Message{
			Role:       am.Role,
			Content:    am.Content,
			ToolCallID: am.ToolCallID,
			Name:       am.Name,
		}
		// Carry tool call details if present.
		if len(am.ToolCalls) > 0 {
			m.ToolCalls = make([]llm.ToolCall, len(am.ToolCalls))
			for i, tc := range am.ToolCalls {
				m.ToolCalls[i] = llm.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: llm.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		result = append(result, m)
	}

	// Truncate if needed.
	if maxTokens > 0 {
		result = truncateContext(result, maxTokens)
	}

	return result, nil
}

// truncateContext drops oldest non-system messages when the estimated token
// count exceeds maxTokens. The system prompt is always preserved.
// Token estimation is approximate (characters / 4).
func truncateContext(messages []llm.Message, maxTokens int) []llm.Message {
	if len(messages) <= 1 {
		return messages
	}

	estimatedTokens := estimateTokens(messages)
	if estimatedTokens <= maxTokens {
		return messages
	}

	// The first message is the system prompt — keep it.
	system := messages[0]
	rest := messages[1:]

	// Drop oldest messages until we're under the limit.
	for len(rest) > 0 {
		// Try without the oldest remaining message.
		candidate := append([]llm.Message{system}, rest[1:]...)
		if estimateTokens(candidate) <= maxTokens {
			return candidate
		}
		rest = rest[1:]
	}

	// All non-system messages dropped; return just the system prompt.
	return []llm.Message{system}
}

// estimateTokens returns a rough token count for a slice of messages.
func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		// Rough: each message has overhead ~4 tokens for role/formatting.
		total += 4
		total += len(m.Content) / approximateTokensPerChar
		for _, tc := range m.ToolCalls {
			total += 8 // overhead per tool call
			total += len(tc.Function.Name) / approximateTokensPerChar
			total += len(tc.Function.Arguments) / approximateTokensPerChar
		}
	}
	return total
}
