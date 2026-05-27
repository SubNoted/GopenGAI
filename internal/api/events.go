package api

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// EventBus — pub/sub for SSE events
// ---------------------------------------------------------------------------

// SSEEvent represents a server-sent event with a type and optional payload.
type SSEEvent struct {
	Type       string      `json:"type"`
	Properties interface{} `json:"properties,omitempty"`
}

// EventBus provides a concurrency-safe publish/subscribe mechanism for SSE
// events. Listeners can subscribe globally (all events) or per-session.
// Slow listeners are protected by a bounded channel buffer — events are
// dropped if the buffer is full.
type EventBus struct {
	mu       sync.RWMutex
	global   []chan SSEEvent
	sessions map[string][]chan SSEEvent
	closed   bool
	done     chan struct{} // closed by Close() to stop heartbeat
}

// eventBufferSize is the capacity of each subscriber's event channel.
// 64 events is enough to buffer bursts while protecting slow consumers.
const eventBufferSize = 64

// NewEventBus creates an EventBus and starts a heartbeat goroutine that
// publishes a "heartbeat" event every 15 seconds.
func NewEventBus() *EventBus {
	eb := &EventBus{
		sessions: make(map[string][]chan SSEEvent),
		done:     make(chan struct{}),
	}
	go eb.heartbeat()
	return eb
}

// Subscribe registers a new listener. If sessionID is non-empty, the
// listener receives only events for that session. If sessionID is empty,
// the listener receives all global events.
// Returns a receive-only channel that the caller should read from in a loop.
// The caller MUST call Unsubscribe when done to prevent goroutine leaks.
//
// If the EventBus has been closed, Subscribe returns a closed channel so that
// the caller's range loop exits immediately (no goroutine leak).
func (eb *EventBus) Subscribe(sessionID string) <-chan SSEEvent {
	ch := make(chan SSEEvent, eventBufferSize)
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		close(ch)
		return ch
	}
	if sessionID == "" {
		eb.global = append(eb.global, ch)
	} else {
		eb.sessions[sessionID] = append(eb.sessions[sessionID], ch)
	}
	return ch
}

// Unsubscribe removes a previously subscribed listener and closes its channel.
// It is safe to call multiple times for the same channel (second call is no-op).
func (eb *EventBus) Unsubscribe(sessionID string, ch <-chan SSEEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if sessionID == "" {
		for i, c := range eb.global {
			if c == ch {
				eb.global = append(eb.global[:i], eb.global[i+1:]...)
				close(c)
				return
			}
		}
		return
	}

	listeners := eb.sessions[sessionID]
	for i, c := range listeners {
		if c == ch {
			if len(listeners) == 1 {
				delete(eb.sessions, sessionID)
			} else {
				eb.sessions[sessionID] = append(listeners[:i], listeners[i+1:]...)
			}
			close(c)
			return
		}
	}
}

// PublishGlobal sends an event to all global listeners (non-blocking).
// Events are dropped if a listener's channel buffer is full.
// Implements the agent.EventBus interface using primitive types.
func (eb *EventBus) PublishGlobal(eventType string, properties interface{}) {
	eb.publishGlobalSSE(SSEEvent{Type: eventType, Properties: properties})
}

// publishGlobalSSE is the internal SSEEvent-based version.
func (eb *EventBus) publishGlobalSSE(event SSEEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, ch := range eb.global {
		select {
		case ch <- event:
		default:
			// Drop event if buffer is full (slow consumer protection).
		}
	}
}

// PublishSession sends an event to all listeners of a specific session,
// plus all global listeners. Publishing is non-blocking.
// Implements the agent.EventBus interface using primitive types.
func (eb *EventBus) PublishSession(sessionID string, eventType string, properties interface{}) {
	eb.publishSessionSSE(sessionID, SSEEvent{Type: eventType, Properties: properties})
}

// publishSessionSSE is the internal SSEEvent-based version.
func (eb *EventBus) publishSessionSSE(sessionID string, event SSEEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	// Session-specific listeners.
	for _, ch := range eb.sessions[sessionID] {
		select {
		case ch <- event:
		default:
		}
	}

	// Global listeners.
	for _, ch := range eb.global {
		select {
		case ch <- event:
		default:
		}
	}
}

// Close shuts down the event bus, closing all listener channels and
// stopping the heartbeat goroutine.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		return
	}
	eb.closed = true
	close(eb.done) // signal heartbeat to stop
	for _, ch := range eb.global {
		close(ch)
	}
	for _, listeners := range eb.sessions {
		for _, ch := range listeners {
			close(ch)
		}
	}
	eb.global = nil
	eb.sessions = nil
	eb.mu.Unlock()
}

// heartbeat publishes a keepalive event every 15 seconds to keep SSE
// connections alive (prevents proxy timeouts). Uses the done channel for
// clean shutdown — no TOCTOU gap between closed-check and publish.
func (eb *EventBus) heartbeat() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-eb.done:
			return
		case <-ticker.C:
			eb.publishGlobalSSE(SSEEvent{Type: "heartbeat"})
		}
	}
}
