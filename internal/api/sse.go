package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// SSE (Server-Sent Events) — real-time event streaming for the async agent
// engine. Clients subscribe via HTTP GET and receive a stream of named events
// in the standard SSE format:
//
//	event: <type>\n
//	data: <json>\n\n
//
// Two subscription modes are available:
//   - GET /events           — global stream (all sessions)
//   - GET /session/{id}/events — session-specific stream
//
// The event types published by the engine are (see internal/agent/engine.go):
//   - session.status          — "working" | "idle"
//   - message.llm.started     — LLM call started
//   - message.part.added      — partial content received
//   - message.part.updated    — content updated
//   - message.tool.started    — tool execution started
//   - message.tool.completed  — tool execution completed
//   - message.tool.error      — tool execution failed
//   - message.complete        — final response ready
//   - message.error           — error during processing
//   - heartbeat               — keepalive (every 15s by EventBus)
// ---------------------------------------------------------------------------

// writeSSE writes a single SSE event to the ResponseWriter in the standard
// format: "event: <type>\ndata: <json>\n\n". It flushes after each event so
// the client receives it in real time. Event type is sanitized to prevent
// newline injection that could break SSE framing.
func writeSSE(w http.ResponseWriter, event SSEEvent) error {
	data, err := json.Marshal(event.Properties)
	if err != nil {
		return fmt.Errorf("marshal SSE data: %w", err)
	}

	// Sanitize event type and data: strip newlines/carriage returns to prevent
	// injected events. SSE protocol uses \n for field separation. While
	// json.Marshal escapes newlines in strings, Properties is interface{} — a
	// json.RawMessage or custom Marshaler could emit bare \n. Defend both fields.
	safeType := strings.ReplaceAll(event.Type, "\n", "")
	safeType = strings.ReplaceAll(safeType, "\r", "")

	safeData := strings.ReplaceAll(string(data), "\n", "")
	safeData = strings.ReplaceAll(safeData, "\r", "")

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", safeType, safeData)
	if err != nil {
		return err
	}

	// Flush so the client sees the event immediately (requires http.Flusher).
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}

// sseHeaders sets the required SSE headers on the response.
// This must be called before any writes to the ResponseWriter.
func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
}

// HandleGlobalSSE subscribes to all global events and streams them to the
// client as SSE. The connection stays open until the client disconnects or
// the EventBus is closed.
//
//	GET /events
func (h *Handler) HandleGlobalSSE(w http.ResponseWriter, r *http.Request) {
	h.streamSSE(w, r, "") // empty sessionID = global listener
}

// HandleSessionSSE subscribes to events for a specific session and streams
// them to the client as SSE. The connection stays open until the client
// disconnects or the EventBus is closed.
//
//	GET /session/{id}/events
func (h *Handler) HandleSessionSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	h.streamSSE(w, r, sessionID)
}

// streamSSE is the shared implementation for both global and session-specific
// SSE handlers. If sessionID is empty, subscribes to global events.
func (h *Handler) streamSSE(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sseHeaders(w)

	// Write an initial comment to confirm the connection is open (some clients
	// or proxies wait for the first byte before processing further events).
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return // client already disconnected
	}
	flusher.Flush()

	// Subscribe.
	ch := h.EventBus.Subscribe(sessionID)
	defer h.EventBus.Unsubscribe(sessionID, ch)

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, event); err != nil {
				if sessionID != "" {
					log.Printf("SSE write error (session %s): %v", sessionID, err)
				} else {
					log.Printf("SSE write error (global): %v", err)
				}
				return
			}
		}
	}
}
