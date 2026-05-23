package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const frontmatterDelimiter = "---"

// ---------------------------------------------------------------------------
// LoadAgent loads a single agent from a .md file with YAML frontmatter.
//
// Expected file format:
//
//	---
//	name: researcher
//	model: "anthropic/claude-sonnet-4-20250514"
//	tools:
//	  - web_fetch
//	permissions:
//	  web_fetch: allow
//	parent_agent: ""
//	---
//
//	You are a research assistant...
//
// The body after the second --- becomes the system prompt.
// If name is not set in frontmatter, it is derived from the filename stem.
// ---------------------------------------------------------------------------

func LoadAgent(path string) (*Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file %s: %w", path, err)
	}

	content := string(data)

	// Parse frontmatter and body.
	fm, body, err := parseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	// Derive name from filename if not set in frontmatter.
	name := fm.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	// Defaults for nil fields.
	if fm.Tools == nil {
		fm.Tools = []string{}
	}
	if fm.Permissions == nil {
		fm.Permissions = make(map[string]string)
	}

	agent := &Agent{
		Name:         name,
		SystemPrompt: strings.TrimSpace(body),
		Model:        fm.Model,
		Tools:        fm.Tools,
		ParentAgent:  fm.ParentAgent,
		Permissions:  fm.Permissions,
		ConfigPath:   path,

		// OpenCode-style fields.
		Description: fm.Description,
		Mode:        fm.Mode,
		Color:       fm.Color,
		AgentPerms:  fm.AgentPerms,
	}

	return agent, nil
}

// LoadDirectory scans a directory for all .md files and loads each as an Agent.
// Returns a map keyed by agent name.
func LoadDirectory(dir string) (map[string]*Agent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read agents directory %s: %w", dir, err)
	}

	agents := make(map[string]*Agent)
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		a, err := LoadAgent(path)
		if err != nil {
			// Skip files that fail to load so a single broken agent
			// doesn't prevent all others from being available.
			continue
		}
		// Duplicate name — last one wins.
		agents[a.Name] = a
		loaded++
	}

	return agents, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// parseFrontmatter splits the content at --- delimiters and extracts YAML
// frontmatter and body text. Returns the parsed frontmatter and body string.
func parseFrontmatter(content string) (*frontmatter, string, error) {
	content = strings.TrimSpace(content)

	// Must start with ---
	if !strings.HasPrefix(content, frontmatterDelimiter) {
		// No frontmatter — entire content is the body.
		return &frontmatter{}, content, nil
	}

	// Remove the opening ---
	rest := strings.TrimPrefix(content, frontmatterDelimiter)
	rest = strings.TrimLeft(rest, "\n\r\t ")

	// Find the closing ---
	idx := strings.Index(rest, "\n"+frontmatterDelimiter)
	if idx == -1 {
		// Also check end-of-file closing (no newline after last ---)
		if strings.HasPrefix(rest, frontmatterDelimiter) {
			// Empty frontmatter: "---\n---\nbody"
			rest = strings.TrimPrefix(rest, frontmatterDelimiter)
			rest = strings.TrimLeft(rest, "\n\r\t ")
			return &frontmatter{}, strings.TrimSpace(rest), nil
		}
		return nil, "", fmt.Errorf("unclosed frontmatter: missing closing %s", frontmatterDelimiter)
	}

	yamlBlock := rest[:idx]
	body := rest[idx+len("\n"+frontmatterDelimiter):]

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, "", fmt.Errorf("yaml parse error: %w", err)
	}

	return &fm, strings.TrimSpace(body), nil
}
