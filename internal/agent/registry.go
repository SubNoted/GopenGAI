package agent

import (
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Registry is an in-memory, concurrency-safe store for agent configurations.
// ---------------------------------------------------------------------------

// Registry holds loaded agent definitions and provides lookup by name.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
}

// NewRegistry creates an empty agent registry.
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]*Agent),
	}
}

// Register adds an agent to the registry. If an agent with the same name
// already exists, it is overwritten.
//
// IMPORTANT: The registry stores the *Agent pointer directly. Callers must
// not mutate an Agent after it has been registered (no concurrency safety
// for the Agent struct itself).
func (r *Registry) Register(agent *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.Name] = agent
}

// Get retrieves an agent by name. Returns an error if the agent is not found.
func (r *Registry) Get(name string) (*Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found in registry", name)
	}
	return a, nil
}

// List returns a copy of all registered agents.
func (r *Registry) List() []Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Agent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, *a)
	}
	return result
}

// Names returns the names of all registered agents.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	return names
}

// Has returns true if an agent with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.agents[name]
	return ok
}

// InitializeFromDir is a convenience wrapper that loads all agents from a
// directory and registers them. Previously registered agents are not cleared.
// Returns the number of agents loaded.
func (r *Registry) InitializeFromDir(dir string) (int, error) {
	loaded, err := LoadDirectory(dir)
	if err != nil {
		return 0, fmt.Errorf("load agents from %s: %w", dir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for name, a := range loaded {
		r.agents[name] = a
	}

	return len(loaded), nil
}

// Size returns the number of registered agents.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}
