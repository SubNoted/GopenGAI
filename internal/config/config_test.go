package config

import (
	"os"
	"path/filepath"
	"testing"
)

// testsEnv wraps setenv + defer-rollback for env variable restoration.
type testsEnv struct {
	t    *testing.T
	prev map[string]string
}

func (e *testsEnv) setenv(key, value string) {
	if e.prev == nil {
		e.prev = make(map[string]string)
	}
	if old, ok := os.LookupEnv(key); ok {
		e.prev[key] = old
	} else {
		e.prev[key] = "" // marker: was not set
	}

	if value == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, value)
	}
}

func (e *testsEnv) cleanup() {
	for k, v := range e.prev {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
}

func TestLoad(t *testing.T) {
	tmp := t.TempDir()

	t.Run("valid config with defaults applied", func(t *testing.T) {
		configPath := filepath.Join(tmp, "valid.json")
		writeFile(t, configPath, `{
			"server": {"port": 3000},
			"llm": {"model": "gpt-4"}
		}`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Server.Port != 3000 {
			t.Errorf("Server.Port = %d, want 3000", cfg.Server.Port)
		}
		if cfg.Server.Host != "0.0.0.0" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
		}
		if cfg.Server.APIKey != "" {
			t.Errorf("Server.APIKey = %q, want empty", cfg.Server.APIKey)
		}
		if cfg.LLM.Model != "gpt-4" {
			t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "gpt-4")
		}
		if cfg.LLM.Provider != "openai" {
			t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "openai")
		}
		if cfg.LLM.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "https://api.openai.com/v1")
		}
		if cfg.LLM.MaxIterations != 10 {
			t.Errorf("LLM.MaxIterations = %d, want 10", cfg.LLM.MaxIterations)
		}
		if cfg.AgentsDir != "./agents" {
			t.Errorf("AgentsDir = %q, want %q", cfg.AgentsDir, "./agents")
		}
		if cfg.DataDir != "./.gopengai" {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, "./.gopengai")
		}
		if cfg.DefaultAgent != "default" {
			t.Errorf("DefaultAgent = %q, want %q", cfg.DefaultAgent, "default")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := Load(filepath.Join(tmp, "nonexistent.json"))
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		configPath := filepath.Join(tmp, "invalid.json")
		writeFile(t, configPath, `not json`)

		_, err := Load(configPath)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})

	t.Run("full config", func(t *testing.T) {
		configPath := filepath.Join(tmp, "full.json")
		writeFile(t, configPath, `{
			"server": {"host": "127.0.0.1", "port": 9000, "api_key": "sk-test"},
			"llm": {"provider": "anthropic", "base_url": "https://api.anthropic.com/v1", "api_key": "sk-ant", "model": "claude-3", "max_iterations": 5},
			"agents_dir": "./my_agents",
			"data_dir": "./mydata",
			"default_agent": "helper"
		}`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Server.Host != "127.0.0.1" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
		}
		if cfg.Server.Port != 9000 {
			t.Errorf("Server.Port = %d, want 9000", cfg.Server.Port)
		}
		if cfg.Server.APIKey != "sk-test" {
			t.Errorf("Server.APIKey = %q, want %q", cfg.Server.APIKey, "sk-test")
		}
		if cfg.LLM.Provider != "anthropic" {
			t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
		}
		if cfg.LLM.BaseURL != "https://api.anthropic.com/v1" {
			t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "https://api.anthropic.com/v1")
		}
		if cfg.LLM.APIKey != "sk-ant" {
			t.Errorf("LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "sk-ant")
		}
		if cfg.LLM.Model != "claude-3" {
			t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-3")
		}
		if cfg.LLM.MaxIterations != 5 {
			t.Errorf("LLM.MaxIterations = %d, want 5", cfg.LLM.MaxIterations)
		}
		if cfg.AgentsDir != "./my_agents" {
			t.Errorf("AgentsDir = %q, want %q", cfg.AgentsDir, "./my_agents")
		}
		if cfg.DataDir != "./mydata" {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, "./mydata")
		}
		if cfg.DefaultAgent != "helper" {
			t.Errorf("DefaultAgent = %q, want %q", cfg.DefaultAgent, "helper")
		}
	})
}

func TestApplyEnvOverrides(t *testing.T) {
	env := &testsEnv{t: t}
	defer env.cleanup()

	t.Run("port override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_PORT", "1234")
		applyEnvOverrides(cfg)

		if cfg.Server.Port != 1234 {
			t.Errorf("Server.Port = %d, want 1234", cfg.Server.Port)
		}
		if cfg.Server.Host != "0.0.0.0" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
		}
	})

	t.Run("host override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_HOST", "localhost")
		applyEnvOverrides(cfg)

		if cfg.Server.Host != "localhost" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "localhost")
		}
	})

	t.Run("llm api key override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_API_KEY", "env-key-123")
		applyEnvOverrides(cfg)

		if cfg.LLM.APIKey != "env-key-123" {
			t.Errorf("LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "env-key-123")
		}
	})

	t.Run("llm model override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_MODEL", "env-model")
		applyEnvOverrides(cfg)

		if cfg.LLM.Model != "env-model" {
			t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "env-model")
		}
	})

	t.Run("llm base url override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_BASE_URL", "https://custom.llm/v1")
		applyEnvOverrides(cfg)

		if cfg.LLM.BaseURL != "https://custom.llm/v1" {
			t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "https://custom.llm/v1")
		}
	})

	t.Run("llm provider override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_PROVIDER", "custom")
		applyEnvOverrides(cfg)

		if cfg.LLM.Provider != "custom" {
			t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "custom")
		}
	})

	t.Run("llm max iterations override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_MAX_ITERATIONS", "3")
		applyEnvOverrides(cfg)

		if cfg.LLM.MaxIterations != 3 {
			t.Errorf("LLM.MaxIterations = %d, want 3", cfg.LLM.MaxIterations)
		}
	})

	t.Run("agents dir override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_AGENTS_DIR", "/custom/agents")
		applyEnvOverrides(cfg)

		if cfg.AgentsDir != "/custom/agents" {
			t.Errorf("AgentsDir = %q, want %q", cfg.AgentsDir, "/custom/agents")
		}
	})

	t.Run("data dir override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_DATA_DIR", "/custom/data")
		applyEnvOverrides(cfg)

		if cfg.DataDir != "/custom/data" {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/custom/data")
		}
	})

	t.Run("default agent override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_DEFAULT_AGENT", "custom-agent")
		applyEnvOverrides(cfg)

		if cfg.DefaultAgent != "custom-agent" {
			t.Errorf("DefaultAgent = %q, want %q", cfg.DefaultAgent, "custom-agent")
		}
	})

	t.Run("api key override", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_API_KEY", "bearer-key-here")
		applyEnvOverrides(cfg)

		if cfg.Server.APIKey != "bearer-key-here" {
			t.Errorf("Server.APIKey = %q, want %q", cfg.Server.APIKey, "bearer-key-here")
		}
	})

	t.Run("all overrides combined", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_PORT", "9999")
		env.setenv("GOPENGAI_HOST", "10.0.0.1")
		env.setenv("GOPENGAI_LLM_API_KEY", "all-key")
		env.setenv("GOPENGAI_LLM_BASE_URL", "https://all.llm/v1")
		env.setenv("GOPENGAI_LLM_MODEL", "all-model")
		env.setenv("GOPENGAI_LLM_PROVIDER", "all-provider")
		env.setenv("GOPENGAI_LLM_MAX_ITERATIONS", "7")
		env.setenv("GOPENGAI_AGENTS_DIR", "/all/agents")
		env.setenv("GOPENGAI_DATA_DIR", "/all/data")
		env.setenv("GOPENGAI_DEFAULT_AGENT", "all-agent")
		env.setenv("GOPENGAI_API_KEY", "all-bearer")
		applyEnvOverrides(cfg)

		checks := []struct {
			field string
			got   string
			want  string
		}{
			{"Host", cfg.Server.Host, "10.0.0.1"},
			{"LLM.APIKey", cfg.LLM.APIKey, "all-key"},
			{"LLM.BaseURL", cfg.LLM.BaseURL, "https://all.llm/v1"},
			{"LLM.Model", cfg.LLM.Model, "all-model"},
			{"LLM.Provider", cfg.LLM.Provider, "all-provider"},
			{"AgentsDir", cfg.AgentsDir, "/all/agents"},
			{"DataDir", cfg.DataDir, "/all/data"},
			{"DefaultAgent", cfg.DefaultAgent, "all-agent"},
			{"Server.APIKey", cfg.Server.APIKey, "all-bearer"},
		}
		if cfg.Server.Port != 9999 {
			t.Errorf("Server.Port = %d, want 9999", cfg.Server.Port)
		}
		if cfg.LLM.MaxIterations != 7 {
			t.Errorf("LLM.MaxIterations = %d, want 7", cfg.LLM.MaxIterations)
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
			}
		}
	})

	t.Run("invalid port ignored", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_PORT", "not-a-number")
		applyEnvOverrides(cfg)

		if cfg.Server.Port != 8080 {
			t.Errorf("Server.Port = %d, want 8080 (default, override ignored)", cfg.Server.Port)
		}
	})

	t.Run("invalid max iterations ignored", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		env.setenv("GOPENGAI_LLM_MAX_ITERATIONS", "NaN")
		applyEnvOverrides(cfg)

		if cfg.LLM.MaxIterations != 10 {
			t.Errorf("LLM.MaxIterations = %d, want 10 (default, override ignored)", cfg.LLM.MaxIterations)
		}
	})

	t.Run("empty env values ignored", func(t *testing.T) {
		cfg := &Config{
			Server: ServerConfig{Host: "keep-me", Port: 42},
			LLM:    LLMConfig{Model: "keep-model"},
		}
		applyDefaults(cfg) // won't override already-set fields

		env.setenv("GOPENGAI_HOST", "")
		env.setenv("GOPENGAI_LLM_MODEL", "")
		applyEnvOverrides(cfg)

		if cfg.Server.Host != "keep-me" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "keep-me")
		}
		if cfg.LLM.Model != "keep-model" {
			t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "keep-model")
		}
	})
}

func TestApplyDefaults(t *testing.T) {
	t.Run("all-zero config gets defaults", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)

		if cfg.Server.Host != "0.0.0.0" {
			t.Error("expected host default")
		}
		if cfg.Server.Port != 8080 {
			t.Error("expected port default")
		}
		if cfg.LLM.Provider != "openai" {
			t.Error("expected provider default")
		}
		if cfg.LLM.BaseURL != "https://api.openai.com/v1" {
			t.Error("expected base_url default")
		}
		if cfg.LLM.Model != "gpt-4o-mini" {
			t.Error("expected model default")
		}
		if cfg.LLM.MaxIterations != 10 {
			t.Error("expected max_iterations default")
		}
		if cfg.AgentsDir != "./agents" {
			t.Error("expected agents_dir default")
		}
		if cfg.DataDir != "./.gopengai" {
			t.Error("expected data_dir default")
		}
		if cfg.DefaultAgent != "default" {
			t.Error("expected default_agent default")
		}
	})

	t.Run("pre-filled fields not overwritten", func(t *testing.T) {
		cfg := &Config{
			Server:       ServerConfig{Host: "custom", Port: 3000},
			LLM:          LLMConfig{Model: "my-model", MaxIterations: 5},
			AgentsDir:    "/my/agents",
			DataDir:      "/my/data",
			DefaultAgent: "my-agent",
		}
		applyDefaults(cfg)

		if cfg.Server.Host != "custom" {
			t.Error("host was overwritten")
		}
		if cfg.Server.Port != 3000 {
			t.Error("port was overwritten")
		}
		if cfg.LLM.Model != "my-model" {
			t.Error("model was overwritten")
		}
		if cfg.LLM.MaxIterations != 5 {
			t.Error("max_iterations was overwritten")
		}
		if cfg.AgentsDir != "/my/agents" {
			t.Error("agents_dir was overwritten")
		}
		if cfg.DataDir != "/my/data" {
			t.Error("data_dir was overwritten")
		}
		if cfg.DefaultAgent != "my-agent" {
			t.Error("default_agent was overwritten")
		}
	})
}

// writeFile is a test helper to create a file.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}
