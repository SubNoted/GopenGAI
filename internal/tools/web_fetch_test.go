package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebFetchTool_Execute(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body><h1>Hello</h1><p>World</p></body></html>"))
		}))
		defer srv.Close()

		tool := &WebFetchTool{}
		args := json.RawMessage(`{"url":"` + srv.URL + `"}`)

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result != "HelloWorld" {
			t.Errorf("result = %q, want %q", result, "HelloWorld")
		}
	})

	t.Run("invalid JSON arguments", func(t *testing.T) {
		tool := &WebFetchTool{}
		_, err := tool.Execute(context.Background(), json.RawMessage(`bad json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("empty URL", func(t *testing.T) {
		tool := &WebFetchTool{}
		_, err := tool.Execute(context.Background(), json.RawMessage(`{"url":""}`))
		if err == nil {
			t.Fatal("expected error for empty URL")
		}
	})

	t.Run("non-http scheme rejected", func(t *testing.T) {
		tool := &WebFetchTool{}
		_, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"file:///etc/passwd"}`))
		if err == nil {
			t.Fatal("expected error for file:// URL (SSRF protection)")
		}
	})

	t.Run("FTP scheme rejected", func(t *testing.T) {
		tool := &WebFetchTool{}
		_, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"ftp://evil.com/malware"}`))
		if err == nil {
			t.Fatal("expected error for ftp:// URL (SSRF protection)")
		}
	})

	t.Run("non-200 response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		tool := &WebFetchTool{}
		args := json.RawMessage(`{"url":"` + srv.URL + `"}`)

		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatal("expected error for 404 response")
		}
	})

	t.Run("response truncation at max display chars", func(t *testing.T) {
		// Generate content longer than 10000 chars.
		longText := make([]byte, 20000)
		for i := range longText {
			longText[i] = 'x'
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(longText)
		}))
		defer srv.Close()

		tool := &WebFetchTool{}
		args := json.RawMessage(`{"url":"` + srv.URL + `"}`)

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should be truncated to 10000 + "... [truncated]"
		if len(result) > 10000+len("... [truncated]")+100 {
			t.Errorf("result too long: %d chars, expected ~10000", len(result))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Would block, but context is cancelled.
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		tool := &WebFetchTool{}
		args := json.RawMessage(`{"url":"` + srv.URL + `"}`)

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("max response bytes limit", func(t *testing.T) {
		bigBody := make([]byte, 2000)
		for i := range bigBody {
			bigBody[i] = 'a'
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(bigBody)
		}))
		defer srv.Close()

		tool := &WebFetchTool{MaxResponseBytes: 100}
		args := json.RawMessage(`{"url":"` + srv.URL + `"}`)

		result, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// With MaxResponseBytes=100, we read at most 100 bytes from body.
		// After HTML stripping, should be short.
		if len(result) > 100 {
			t.Errorf("result %d bytes, should be <= 100 (MaxResponseBytes)", len(result))
		}
	})

	t.Run("tool metadata", func(t *testing.T) {
		tool := &WebFetchTool{}
		if tool.Name() != "web_fetch" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "web_fetch")
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if len(params) == 0 {
			t.Error("Parameters() should not be empty")
		}
	})
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic tags",
			input:    "<h1>Title</h1><p>Text</p>",
			expected: "TitleText",
		},
		{
			name:     "script tag removed with content",
			input:    "<p>Before</p><script>alert('xss')</script><p>After</p>",
			expected: "BeforeAfter",
		},
		{
			name:     "style tag removed with content",
			input:    "<p>Text</p><style>body { color: red; }</style><p>More</p>",
			expected: "TextMore",
		},
		{
			name:     "HTML entities decoded",
			input:    "Price: &lt; &gt; &amp; &quot;",
			expected: `Price: < > & "`,
		},
		{
			name:     "whitespace collapsed",
			input:    "Hello    world\n\n\n  \t\t  test",
			expected: "Hello world test",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "no HTML tags",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "script tag with attributes",
			input:    `<p>Hi</p><script type="text/javascript">var x=1;</script><p>Bye</p>`,
			expected: "HiBye",
		},
		{
			name:     "nested tags",
			input:    "<div><p>Hello <b>World</b></p></div>",
			expected: "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.expected {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStripScriptStyle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes script block",
			input:    "aa<script>evil</script>bb",
			expected: "aabb",
		},
		{
			name:     "removes style block",
			input:    "aa<style>h1{color:red}</style>bb",
			expected: "aabb",
		},
		{
			name:     "no script or style",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "script with attributes",
			input:    `aa<script src="x.js">code</script>bb`,
			expected: "aabb",
		},
		{
			name:     "unclosed script tag",
			input:    "aa<script>never closes",
			expected: "aanever closes",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripScriptStyle(tt.input)
			if got != tt.expected {
				t.Errorf("stripScriptStyle(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
