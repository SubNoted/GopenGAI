package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Config is the top-level configuration matching gopengai.json schema.
type Config struct {
	Server       ServerConfig `json:"server"`
	LLM          LLMConfig    `json:"llm"`
	AgentsDir    string       `json:"agents_dir"`
	DataDir      string       `json:"data_dir"`
	DefaultAgent string       `json:"default_agent"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	Model         string `json:"model"`
	MaxIterations int    `json:"max_iterations"`
}

// Load reads a JSON config file from path and returns a Config with defaults applied.
// Environment variable overrides are applied after JSON loading and defaults:
//
//	GOPENGAI_PORT, GOPENGAI_HOST, GOPENGAI_LLM_API_KEY, GOPENGAI_LLM_BASE_URL,
//	GOPENGAI_LLM_MODEL, GOPENGAI_LLM_PROVIDER, GOPENGAI_LLM_MAX_ITERATIONS,
//	GOPENGAI_AGENTS_DIR, GOPENGAI_DATA_DIR, GOPENGAI_DEFAULT_AGENT
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// Apply defaults (lowest priority)
	applyDefaults(&cfg)

	// Apply environment variable overrides (medium priority)
	applyEnvOverrides(&cfg)

	return &cfg, nil
}

// applyDefaults sets default values for all zero-valued config fields.
func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "openai"
	}
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "gpt-4o-mini"
	}
	if cfg.LLM.MaxIterations == 0 {
		cfg.LLM.MaxIterations = 10
	}
	if cfg.AgentsDir == "" {
		cfg.AgentsDir = "./agents"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./.gopengai"
	}
	if cfg.DefaultAgent == "" {
		cfg.DefaultAgent = "default"
	}
}

// applyEnvOverrides reads environment variables and overrides config fields.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GOPENGAI_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("GOPENGAI_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("GOPENGAI_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("GOPENGAI_LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("GOPENGAI_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("GOPENGAI_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("GOPENGAI_LLM_MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LLM.MaxIterations = n
		}
	}
	if v := os.Getenv("GOPENGAI_AGENTS_DIR"); v != "" {
		cfg.AgentsDir = v
	}
	if v := os.Getenv("GOPENGAI_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("GOPENGAI_DEFAULT_AGENT"); v != "" {
		cfg.DefaultAgent = v
	}
}
