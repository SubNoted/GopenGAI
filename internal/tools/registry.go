package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Tool interface
// ---------------------------------------------------------------------------

// Tool is the interface that all tools must implement.
// Each tool has a name, description, JSON Schema for parameters, and an
// Execute method that performs the tool's action.
type Tool interface {
	// Name returns the unique name of the tool (e.g. "web_fetch", "memory_save").
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// Parameters returns a JSON Schema describing the tool's input parameters.
	Parameters() json.RawMessage

	// Execute runs the tool with the given JSON-encoded arguments.
	// It returns the result as a string (usually plain text or JSON).
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// ---------------------------------------------------------------------------
// Tool Registry
// ---------------------------------------------------------------------------

// Registry is a concurrency-safe collection of tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. If a tool with the same name already
// exists, it is overwritten.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name. Returns an error if the tool is not found.
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found in registry", name)
	}
	return t, nil
}

// List returns a copy of all registered tool names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Size returns the number of registered tools.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ToToolDefinitions converts all registered tools to the LLM API format
// ([]llm.ToolDefinition). Returns an empty slice if no tools are registered.
func (r *Registry) ToToolDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tools) == 0 {
		return []ToolDefinition{}
	}

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, ToolDefinition{
			Type: "function",
			Function: ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return defs
}

// ---------------------------------------------------------------------------
// Permission checking
// ---------------------------------------------------------------------------

// IsAllowed checks whether a tool is allowed given an agent's permissions map.
// The permissions map maps tool names to "allow" or "deny".
//
// Rules:
//   - If permissions is nil or empty, the tool is allowed (default-allow).
//   - If the tool name is explicitly set to "allow", it is allowed.
//   - If the tool name is explicitly set to "deny", it is denied.
//   - If the tool name is not in the permissions map, it is denied (default-deny
//     for explicit permissions).
func IsAllowed(toolName string, permissions map[string]string) bool {
	if len(permissions) == 0 {
		return true
	}
	perm, ok := permissions[toolName]
	if !ok {
		return false
	}
	return perm == "allow"
}

// ---------------------------------------------------------------------------
// LLM-compatible types (subset of llm package types, mirrors them to avoid
// circular dependencies)
// ---------------------------------------------------------------------------

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
