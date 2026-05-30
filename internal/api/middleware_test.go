package api

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	t.Run("no api key — pass through", func(t *testing.T) {
		mw := AuthMiddleware("")
		req := httptest.NewRequest("GET", "/anything", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("valid api key", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/anything", nil)
		req.Header.Set("Authorization", "Bearer secret-key")
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("invalid api key", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/anything", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "unauthorized") {
			t.Errorf("body = %q, want 'unauthorized'", string(body))
		}
	})

	t.Run("missing authorization header", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/anything", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("health endpoint bypasses auth", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("wrong auth scheme (Basic instead of Bearer)", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/anything", nil)
		req.Header.Set("Authorization", "Basic secret-key")
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("empty bearer token", func(t *testing.T) {
		mw := AuthMiddleware("secret-key")
		req := httptest.NewRequest("GET", "/anything", nil)
		req.Header.Set("Authorization", "Bearer ")
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestCORSMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("OPTIONS preflight returns 204", func(t *testing.T) {
		mw := CORSMiddleware
		req := httptest.NewRequest("OPTIONS", "/anything", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("GET passes through with CORS headers", func(t *testing.T) {
		mw := CORSMiddleware
		req := httptest.NewRequest("GET", "/anything", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing Access-Control-Allow-Origin header")
		}
	})

	t.Run("Vary header set", func(t *testing.T) {
		mw := CORSMiddleware
		req := httptest.NewRequest("GET", "/anything", nil)
		rec := httptest.NewRecorder()
		mw(handler).ServeHTTP(rec, req)

		if rec.Header().Get("Vary") != "Origin" {
			t.Error("missing Vary: Origin header")
		}
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("normal handler passes through", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		RecoveryMiddleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("panicking handler returns 500", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		RecoveryMiddleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("panic after headers written does not try 500", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("partial"))
			panic("mid-stream panic")
		})
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		RecoveryMiddleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (already committed)", rec.Code)
		}
	})
}

func TestLoggingMiddleware(t *testing.T) {
	t.Run("logs request info", func(t *testing.T) {
		// Capture log output.
		var buf strings.Builder
		orig := log.Writer()
		log.SetOutput(&buf)
		defer log.SetOutput(orig)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest("GET", "/test/path", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		LoggingMiddleware(handler).ServeHTTP(rec, req)

		output := buf.String()
		if !strings.Contains(output, "GET") {
			t.Error("log missing method")
		}
		if !strings.Contains(output, "/test/path") {
			t.Error("log missing path")
		}
		if !strings.Contains(output, "200") {
			t.Error("log missing status code")
		}
	})
}

func TestApplyMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 — unique marker
	})

	t.Run("single middleware", func(t *testing.T) {
		wrapped := ApplyMiddleware(handler, CORSMiddleware)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusTeapot {
			t.Errorf("status = %d, want 418", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("CORS middleware not applied")
		}
	})

	t.Run("chain three middleware", func(t *testing.T) {
		wrapped := ApplyMiddleware(handler,
			RecoveryMiddleware,
			CORSMiddleware,
			AuthMiddleware(""),
		)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusTeapot {
			t.Errorf("status = %d, want 418", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("CORS not applied")
		}
	})
}
