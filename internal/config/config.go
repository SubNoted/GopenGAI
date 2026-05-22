package config

import (
	"encoding/json"
	"fmt"
	"os"
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
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// Apply defaults
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

	return &cfg, nil
}
