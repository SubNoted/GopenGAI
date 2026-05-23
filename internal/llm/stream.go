package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// SSE event parsing
// ---------------------------------------------------------------------------

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string // event type (e.g., "message", "done")
	Data  string // raw data payload
}

// ParseSSEStream reads an SSE stream line by line and yields parsed events.
// Returns when the stream is exhausted or ctx is cancelled.
func ParseSSEStream(ctx context.Context, r io.Reader, out chan<- SSEEvent) error {
	defer close(out)

	scanner := bufio.NewScanner(r)
	// SSE lines can be up to several KB; allow a large buffer.
	scanner.Buffer(make([]byte, 0, 65536), 65536)

	var current SSEEvent
	for scanner.Scan() {
		line := scanner.Text()

		// An empty line signals the end of an event.
		if line == "" {
			if current.Data != "" {
				select {
				case out <- current:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			current = SSEEvent{}
			continue
		}

		// Ignore comments (lines starting with ":").
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse "event: ..." or "data: ..." lines.
		if strings.HasPrefix(line, "event:") {
			current.Event = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			// Data lines may be concatenated with newlines for multi-line data.
			data := strings.TrimSpace(line[5:])
			if current.Data != "" {
				current.Data += "\n"
			}
			current.Data += data
		}
		// "id:", "retry:" are ignored for now.
	}

	return scanner.Err()
}

// ---------------------------------------------------------------------------
// Streaming completion
// ---------------------------------------------------------------------------

// StreamCompletion sends a streaming chat completion request to the LLM provider.
// It returns a channel of SSEEvent and an error channel.
//
// Usage:
//
//	events, errs := client.StreamCompletion(ctx, req)
//	for event := range events {
//	    // parse event.Data as StreamCompletionResponse or StreamDelta
//	}
//	if err := <-errs; err != nil {
//	    // handle error
//	}
//
// NOTE: This is a streaming (SSE-based) completion. Set req.Stream = true
// on providers that support it (OpenAI, Anthropic via /v1/messages?stream=true, etc.).
func (c *Client) StreamCompletion(ctx context.Context, req *ChatCompletionRequest) (<-chan SSEEvent, <-chan error) {
	events := make(chan SSEEvent, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(errs)
		defer close(events)

		if req.Model == "" {
			req.Model = c.Model
		}

		body, err := json.Marshal(req)
		if err != nil {
			errs <- fmt.Errorf("marshal stream request: %w", err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			errs <- fmt.Errorf("create stream request: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")
		httpReq.Header.Set("Connection", "keep-alive")
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			errs <- fmt.Errorf("http stream request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errs <- fmt.Errorf("stream api error (status %d): %s", resp.StatusCode, string(bodyBytes))
			return
		}

		if err := ParseSSEStream(ctx, resp.Body, events); err != nil {
			errs <- fmt.Errorf("parse sse stream: %w", err)
			return
		}

		errs <- nil
	}()

	return events, errs
}

// ParseStreamData parses the Data field of an SSEEvent as a StreamCompletionResponse.
func ParseStreamData(data string) (*StreamCompletionResponse, error) {
	var chunk StreamCompletionResponse
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, fmt.Errorf("unmarshal stream chunk: %w", err)
	}
	return &chunk, nil
}

// IsStreamDone checks if a stream chunk contains a finish_reason.
func IsStreamDone(chunk *StreamCompletionResponse) bool {
	for _, c := range chunk.Choices {
		if c.FinishReason != "" {
			return true
		}
	}
	return false
}
