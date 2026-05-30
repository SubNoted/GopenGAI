package api

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

// ---------------------------------------------------------------------------
// Middleware — standard HTTP middleware for the GoPengAI API server.
//
// Three middleware functions are provided:
//   - LoggingMiddleware   — logs method, path, status, duration per request
//   - CORSMiddleware      — sets CORS headers, handles OPTIONS preflight
//   - RecoveryMiddleware  — catches panics, logs stack trace, returns 500
//
// Use ApplyMiddleware to chain them onto any http.Handler.
// ---------------------------------------------------------------------------

// responseWriter wraps http.ResponseWriter to capture the status code for
// logging purposes. It forwards optional interfaces (http.Flusher, http.Hijacker,
// http.Pusher) to the underlying writer so middleware doesn't break handlers
// that rely on them (SSE, WebSocket, HTTP/2 push).
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher for SSE support.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket upgrades.
func (rw *responseWriter) Hijack() (interface { /* net.Conn */
}, interface { /* *bufio.ReadWriter */
}, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push implements http.Pusher for HTTP/2 server push.
func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := rw.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// LoggingMiddleware logs every HTTP request with method, path, remote address,
// response status code, and duration.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %s %d %s",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			rw.statusCode,
			time.Since(start).Round(time.Microsecond),
		)
	})
}

// CORSMiddleware sets permissive CORS headers and handles preflight OPTIONS
// requests.
//
// WARNING: Access-Control-Allow-Origin: * permits any website to read API
// responses cross-origin. This is acceptable while no authentication layer
// exists, but MUST be restricted to a specific origin (or an allowlist)
// before adding auth tokens, API keys, or cookies to the API.
// See: https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS/Errors/CORSMissingAllowOrigin
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Set("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware catches panics in downstream handlers, logs the stack
// trace, and attempts to return a 500 Internal Server Error to the client.
// If the response headers have already been sent (e.g., a handler panicked
// mid-response-body), the 500 cannot be written and is silently skipped.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v\n%s", rec, debug.Stack())
				// Don't attempt to write 500 if headers already committed — the
				// client already received a partial response and a 500 would be
				// ignored by net/http.
				if !rw.wroteHeader {
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// ApplyMiddleware chains middleware functions onto a handler. The first
// middleware in the list is the outermost (applied last, runs first).
//
//	Example: ApplyMiddleware(mux, RecoveryMiddleware, CORSMiddleware, LoggingMiddleware)
func ApplyMiddleware(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
