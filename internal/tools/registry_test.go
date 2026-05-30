package tools

import (
	"testing"
)

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name        string
		permissions map[string]string
		toolName    string
		expected    bool
	}{
		{
			name:        "nil permissions — default allow",
			permissions: nil,
			toolName:    "any_tool",
			expected:    true,
		},
		{
			name:        "empty permissions — default allow",
			permissions: map[string]string{},
			toolName:    "any_tool",
			expected:    true,
		},
		{
			name:        "explicit allow",
			permissions: map[string]string{"web_fetch": "allow"},
			toolName:    "web_fetch",
			expected:    true,
		},
		{
			name:        "explicit deny",
			permissions: map[string]string{"web_fetch": "deny"},
			toolName:    "web_fetch",
			expected:    false,
		},
		{
			name:        "not in map — default deny with explicit permissions",
			permissions: map[string]string{"web_fetch": "allow"},
			toolName:    "memory_save",
			expected:    false,
		},
		{
			name:        "mixed permissions",
			permissions: map[string]string{"web_fetch": "allow", "delegate": "deny"},
			toolName:    "delegate",
			expected:    false,
		},
		{
			name:        "unknown value treated as deny",
			permissions: map[string]string{"web_fetch": "unknown_value"},
			toolName:    "web_fetch",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAllowed(tt.toolName, tt.permissions)
			if got != tt.expected {
				t.Errorf("IsAllowed(%q, %v) = %v, want %v",
					tt.toolName, tt.permissions, got, tt.expected)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	t.Run("register and retrieve", func(t *testing.T) {
		r := NewRegistry()
		wf := &WebFetchTool{MaxResponseBytes: 1024}
		r.Register(wf)

		got, err := r.Get("web_fetch")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Name() != "web_fetch" {
			t.Errorf("Name() = %q, want %q", got.Name(), "web_fetch")
		}
	})

	t.Run("get non-existent tool", func(t *testing.T) {
		r := NewRegistry()
		_, err := r.Get("nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent tool")
		}
	})

	t.Run("list tools", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&WebFetchTool{})
		r.Register(&MemorySave{})

		names := r.List()
		if len(names) != 2 {
			t.Errorf("len(List()) = %d, want 2", len(names))
		}
	})

	t.Run("size", func(t *testing.T) {
		r := NewRegistry()
		if r.Size() != 0 {
			t.Errorf("Size() = %d, want 0", r.Size())
		}
		r.Register(&WebFetchTool{})
		if r.Size() != 1 {
			t.Errorf("Size() = %d, want 1", r.Size())
		}
	})

	t.Run("to tool definitions", func(t *testing.T) {
		r := NewRegistry()
		defs := r.ToToolDefinitions()
		if len(defs) != 0 {
			t.Errorf("len(ToToolDefinitions()) = %d, want 0", len(defs))
		}

		r.Register(&WebFetchTool{})
		defs = r.ToToolDefinitions()
		if len(defs) != 1 {
			t.Errorf("len(ToToolDefinitions()) = %d, want 1", len(defs))
		}
		if defs[0].Type != "function" {
			t.Errorf("Type = %q, want %q", defs[0].Type, "function")
		}
		if defs[0].Function.Name != "web_fetch" {
			t.Errorf("Function.Name = %q, want %q", defs[0].Function.Name, "web_fetch")
		}
	})

	t.Run("register overwrites duplicate", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&WebFetchTool{MaxResponseBytes: 100})
		r.Register(&WebFetchTool{MaxResponseBytes: 200})

		got, _ := r.Get("web_fetch")
		wf := got.(*WebFetchTool)
		if wf.MaxResponseBytes != 200 {
			t.Errorf("MaxResponseBytes = %d, want 200 (overwritten)", wf.MaxResponseBytes)
		}
	})
}
