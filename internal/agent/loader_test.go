package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgent(t *testing.T) {
	tmp := t.TempDir()

	t.Run("full frontmatter with all fields", func(t *testing.T) {
		path := filepath.Join(tmp, "full.md")
		content := `---
name: researcher
model: gpt-4
tools:
  - web_fetch
  - memory_save
permissions:
  web_fetch: allow
  memory_save: allow
parent_agent: ""
description: "A research agent"
mode: "subagent"
color: "#2196F3"
---
You are a research assistant. Always cite sources.`

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "researcher" {
			t.Errorf("Name = %q, want %q", a.Name, "researcher")
		}
		if a.SystemPrompt != "You are a research assistant. Always cite sources." {
			t.Errorf("SystemPrompt = %q", a.SystemPrompt)
		}
		if a.Model != "gpt-4" {
			t.Errorf("Model = %q, want %q", a.Model, "gpt-4")
		}
		if len(a.Tools) != 2 {
			t.Errorf("len(Tools) = %d, want 2", len(a.Tools))
		}
		if a.ParentAgent != "" {
			t.Errorf("ParentAgent = %q, want empty", a.ParentAgent)
		}
		if a.Permissions["web_fetch"] != "allow" {
			t.Errorf("Permissions[web_fetch] = %q, want %q", a.Permissions["web_fetch"], "allow")
		}
		if a.Permissions["memory_save"] != "allow" {
			t.Errorf("Permissions[memory_save] = %q, want %q", a.Permissions["memory_save"], "allow")
		}
		if a.ConfigPath != path {
			t.Errorf("ConfigPath = %q, want %q", a.ConfigPath, path)
		}
		// OpenCode-style fields.
		if a.Description != "A research agent" {
			t.Errorf("Description = %q, want %q", a.Description, "A research agent")
		}
		if a.Mode != "subagent" {
			t.Errorf("Mode = %q, want %q", a.Mode, "subagent")
		}
		if a.Color != "#2196F3" {
			t.Errorf("Color = %q, want %q", a.Color, "#2196F3")
		}
	})

	t.Run("name derived from filename when not in frontmatter", func(t *testing.T) {
		path := filepath.Join(tmp, "derived-name.md")
		content := `---
---
I am a helper.`

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "derived-name" {
			t.Errorf("Name = %q, want %q", a.Name, "derived-name")
		}
	})

	t.Run("no frontmatter — entire content is system prompt", func(t *testing.T) {
		path := filepath.Join(tmp, "nofm.md")
		content := "You are a helpful assistant without any frontmatter."

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "nofm" {
			t.Errorf("Name = %q, want %q", a.Name, "nofm")
		}
		if a.SystemPrompt != "You are a helpful assistant without any frontmatter." {
			t.Errorf("SystemPrompt = %q", a.SystemPrompt)
		}
		if len(a.Tools) != 0 {
			t.Errorf("len(Tools) = %d, want 0", len(a.Tools))
		}
	})

	t.Run("empty frontmatter with body", func(t *testing.T) {
		path := filepath.Join(tmp, "empty-fm.md")
		content := `---
---
Just a body.`

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "empty-fm" {
			t.Errorf("Name = %q", a.Name)
		}
		if a.SystemPrompt != "Just a body." {
			t.Errorf("SystemPrompt = %q", a.SystemPrompt)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadAgent(filepath.Join(tmp, "no-such-file.md"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("unclosed frontmatter", func(t *testing.T) {
		path := filepath.Join(tmp, "unclosed.md")
		content := `---
name: broken
tools:
  - thing
no closing delimiter!`

		writeTestFile(t, path, content)

		_, err := LoadAgent(path)
		if err == nil {
			t.Fatal("expected error for unclosed frontmatter")
		}
	})

	t.Run("openCode-style structured permissions", func(t *testing.T) {
		path := filepath.Join(tmp, "oc-perms.md")
		content := `---
name: editor
description: "File editor agent"
mode: "primary"
color: "#FF5722"
permission:
  read: {"/src/**": allow}
  write: {"/src/**": ask}
  edit: {"/src/**": allow}
  bash: {"/usr/bin/git": allow}
  glob: allow
  grep: allow
  task: {researcher: allow}
---
You are a code editor with file access.`

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "editor" {
			t.Errorf("Name = %q, want %q", a.Name, "editor")
		}
		if a.AgentPerms == nil {
			t.Fatal("AgentPerms should not be nil")
		}
		if a.AgentPerms.Read["/src/**"] != "allow" {
			t.Errorf("Read perm = %q", a.AgentPerms.Read["/src/**"])
		}
		if a.AgentPerms.Glob != "allow" {
			t.Errorf("Glob = %q, want %q", a.AgentPerms.Glob, "allow")
		}
		if a.AgentPerms.Task["researcher"] != "allow" {
			t.Errorf("Task perm = %q", a.AgentPerms.Task["researcher"])
		}
	})

	t.Run("invalid YAML in frontmatter", func(t *testing.T) {
		path := filepath.Join(tmp, "bad-yaml.md")
		content := `---
name: [unclosed
tools: bad
---
body`

		writeTestFile(t, path, content)

		_, err := LoadAgent(path)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})

	t.Run("trailing whitespace in frontmatter is handled", func(t *testing.T) {
		path := filepath.Join(tmp, "whitespace.md")
		content := "---\nname: clean\n---\n\nPrompt text\n"

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "clean" {
			t.Errorf("Name = %q, want %q", a.Name, "clean")
		}
		if a.SystemPrompt != "Prompt text" {
			t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "Prompt text")
		}
	})

	t.Run("frontmatter with end-of-file closing delimiter", func(t *testing.T) {
		path := filepath.Join(tmp, "eof-close.md")
		content := "---\nname: eof\n---\nBody"

		writeTestFile(t, path, content)

		a, err := LoadAgent(path)
		if err != nil {
			t.Fatalf("LoadAgent() error = %v", err)
		}
		if a.Name != "eof" {
			t.Errorf("Name = %q, want %q", a.Name, "eof")
		}
		if a.SystemPrompt != "Body" {
			t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "Body")
		}
	})
}

func TestLoadDirectory(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		agents, err := LoadDirectory(dir)
		if err != nil {
			t.Fatalf("LoadDirectory() error = %v", err)
		}
		if len(agents) != 0 {
			t.Errorf("len(agents) = %d, want 0", len(agents))
		}
	})

	t.Run("directory with valid agents", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "a1.md"), "---\nname: agent1\n---\nPrompt 1")
		writeTestFile(t, filepath.Join(dir, "a2.md"), "---\nname: agent2\n---\nPrompt 2")

		agents, err := LoadDirectory(dir)
		if err != nil {
			t.Fatalf("LoadDirectory() error = %v", err)
		}
		if len(agents) != 2 {
			t.Errorf("len(agents) = %d, want 2", len(agents))
		}
		if agents["agent1"].SystemPrompt != "Prompt 1" {
			t.Errorf("agent1 SystemPrompt = %q", agents["agent1"].SystemPrompt)
		}
		if agents["agent2"].SystemPrompt != "Prompt 2" {
			t.Errorf("agent2 SystemPrompt = %q", agents["agent2"].SystemPrompt)
		}
	})

	t.Run("skips non-.md files", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "agent.md"), "---\nname: real\n---\nok")
		writeTestFile(t, filepath.Join(dir, "notes.txt"), "not an agent")

		agents, err := LoadDirectory(dir)
		if err != nil {
			t.Fatalf("LoadDirectory() error = %v", err)
		}
		if len(agents) != 1 {
			t.Errorf("len(agents) = %d, want 1", len(agents))
		}
	})

	t.Run("skips broken agents without error", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "good.md"), "---\nname: good\n---\nprompt")
		writeTestFile(t, filepath.Join(dir, "broken.md"), "---\nname: [bad yaml\n---\nbody")

		agents, err := LoadDirectory(dir)
		if err != nil {
			t.Fatalf("LoadDirectory() error = %v", err)
		}
		if len(agents) != 1 {
			t.Errorf("len(agents) = %d, want 1", len(agents))
		}
		if agents["good"] == nil {
			t.Error("good agent should be loaded")
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadDirectory(filepath.Join(dir, "does-not-exist"))
		if err == nil {
			t.Fatal("expected error for missing directory")
		}
	})

	t.Run("skips subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		subDir := filepath.Join(dir, "sub")
		os.Mkdir(subDir, 0o755)
		writeTestFile(t, filepath.Join(subDir, "sub_agent.md"), "---\nname: sub\n---\nsub")

		agents, err := LoadDirectory(dir)
		if err != nil {
			t.Fatalf("LoadDirectory() error = %v", err)
		}
		// The subdirectory entry is skipped by LoadDirectory.
		if _, exists := agents["sub"]; exists {
			t.Error("subdirectory agent should NOT be loaded (not recursive)")
		}
	})
}

func TestAgent_HasTool(t *testing.T) {
	a := &Agent{Tools: []string{"web_fetch", "memory_save"}}
	if !a.HasTool("web_fetch") {
		t.Error("HasTool('web_fetch') should be true")
	}
	if a.HasTool("nonexistent") {
		t.Error("HasTool('nonexistent') should be false")
	}
	if a.HasTool("") {
		t.Error("HasTool('') should be false")
	}
}

func TestAgent_IsToolAllowed(t *testing.T) {
	t.Run("empty permissions — allowed if in Tools list", func(t *testing.T) {
		a := &Agent{Tools: []string{"web_fetch"}, Permissions: nil}
		if !a.IsToolAllowed("web_fetch") {
			t.Error("IsToolAllowed('web_fetch') with nil Permissions should be true")
		}
		if a.IsToolAllowed("not_in_list") {
			t.Error("IsToolAllowed('not_in_list') with nil Permissions should be false")
		}
	})

	t.Run("explicit allow", func(t *testing.T) {
		a := &Agent{Permissions: map[string]string{"web_fetch": "allow"}}
		if !a.IsToolAllowed("web_fetch") {
			t.Error("IsToolAllowed('web_fetch') with allow should be true")
		}
	})

	t.Run("explicit deny", func(t *testing.T) {
		a := &Agent{Permissions: map[string]string{"web_fetch": "deny"}}
		if a.IsToolAllowed("web_fetch") {
			t.Error("IsToolAllowed('web_fetch') with deny should be false")
		}
	})

	t.Run("not in permissions map — denied", func(t *testing.T) {
		a := &Agent{Permissions: map[string]string{"memory_save": "allow"}}
		if a.IsToolAllowed("web_fetch") {
			t.Error("IsToolAllowed('web_fetch') not in map should be false")
		}
	})
}

func TestRegistry(t *testing.T) {
	t.Run("register and get", func(t *testing.T) {
		r := NewRegistry()
		a := &Agent{Name: "test", Tools: []string{"web_fetch"}}
		r.Register(a)

		got, err := r.Get("test")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Name != "test" {
			t.Errorf("Name = %q", got.Name)
		}
	})

	t.Run("get non-existent", func(t *testing.T) {
		r := NewRegistry()
		_, err := r.Get("nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent agent")
		}
	})

	t.Run("list returns copy", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&Agent{Name: "a1"})
		r.Register(&Agent{Name: "a2"})

		list := r.List()
		if len(list) != 2 {
			t.Errorf("len(list) = %d, want 2", len(list))
		}
		// Verify it's a copy (modifying list doesn't affect registry).
		list[0].Name = "changed"
		a1, _ := r.Get("a1")
		if a1.Name != "a1" {
			t.Error("List returned references, not copies")
		}
	})

	t.Run("names", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&Agent{Name: "a"})
		r.Register(&Agent{Name: "b"})

		names := r.Names()
		if len(names) != 2 {
			t.Errorf("len(names) = %d, want 2", len(names))
		}
		// Both names should be present (order doesn't matter).
		found := map[string]bool{"a": false, "b": false}
		for _, n := range names {
			found[n] = true
		}
		for name, present := range found {
			if !present {
				t.Errorf("name %q not in Names()", name)
			}
		}
	})

	t.Run("has", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&Agent{Name: "exists"})
		if !r.Has("exists") {
			t.Error("Has('exists') should be true")
		}
		if r.Has("nope") {
			t.Error("Has('nope') should be false")
		}
	})

	t.Run("size", func(t *testing.T) {
		r := NewRegistry()
		if r.Size() != 0 {
			t.Error("new registry size should be 0")
		}
		r.Register(&Agent{Name: "a"})
		r.Register(&Agent{Name: "b"})
		if r.Size() != 2 {
			t.Errorf("Size() = %d, want 2", r.Size())
		}
	})

	t.Run("register overwrite", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&Agent{Name: "dup", SystemPrompt: "first"})
		r.Register(&Agent{Name: "dup", SystemPrompt: "second"})

		a, _ := r.Get("dup")
		if a.SystemPrompt != "second" {
			t.Errorf("SystemPrompt = %q, want 'second' (overwritten)", a.SystemPrompt)
		}
	})

	t.Run("InitializeFromDir", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "alpha.md"), "---\nname: alpha\n---\nprompt a")
		writeTestFile(t, filepath.Join(dir, "beta.md"), "---\nname: beta\n---\nprompt b")

		r := NewRegistry()
		n, err := r.InitializeFromDir(dir)
		if err != nil {
			t.Fatalf("InitializeFromDir() error = %v", err)
		}
		if n != 2 {
			t.Errorf("loaded count = %d, want 2", n)
		}
		if !r.Has("alpha") {
			t.Error("alpha not registered")
		}
		if !r.Has("beta") {
			t.Error("beta not registered")
		}
	})
}

// writeTestFile is a test helper for creating files in tests.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile(%s): %v", path, err)
	}
}
