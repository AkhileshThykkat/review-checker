// Package config loads and validates the consumer repo's .review-checker.yaml.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LLM describes how to reach an OpenAI-compatible chat completions endpoint.
type LLM struct {
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	Model     string `yaml:"model"`
}

// Config is the parsed .review-checker.yaml.
type Config struct {
	LLM  LLM    `yaml:"llm"`
	Mode string `yaml:"mode"`

	// Ignore extends the built-in default ignore globs.
	Ignore []string `yaml:"ignore"`

	// CustomRules are repo-specific review rules appended to the generic
	// rules baked into the binary.
	CustomRules []string `yaml:"custom_rules"`

	// MaxFileTokens is the approximate per-file token budget before a
	// file's patch is truncated in the prompt.
	MaxFileTokens int `yaml:"max_file_tokens"`
}

// DefaultIgnore is always applied; repo config extends it.
var DefaultIgnore = []string{
	"**/migrations/**",
	"*.lock",
	"**/*.lock",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"go.sum",
	"vendor/**",
	"node_modules/**",
	"**/*.min.js",
	"**/*.min.css",
	"**/*_pb2.py",
	"**/*.pb.go",
	"**/*.generated.*",
	"dist/**",
	"build/**",
}

const (
	ModeCommentOnly = "comment_only"

	defaultMaxFileTokens = 8000
)

// Load reads, parses, and validates the config file at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw, path)
}

// Parse validates raw YAML config bytes; path is used in error messages only.
func Parse(raw []byte, path string) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if cfg.LLM.BaseURL == "" {
		return nil, fmt.Errorf("%s: llm.base_url is required", path)
	}
	if cfg.LLM.APIKeyEnv == "" {
		return nil, fmt.Errorf("%s: llm.api_key_env is required", path)
	}
	if cfg.LLM.Model == "" {
		return nil, fmt.Errorf("%s: llm.model is required", path)
	}

	if cfg.Mode == "" {
		cfg.Mode = ModeCommentOnly
	}
	if cfg.Mode != ModeCommentOnly {
		return nil, fmt.Errorf("%s: mode %q not supported in v1 (only %q)", path, cfg.Mode, ModeCommentOnly)
	}
	if cfg.MaxFileTokens <= 0 {
		cfg.MaxFileTokens = defaultMaxFileTokens
	}

	return cfg, nil
}

// IgnoreGlobs returns the built-in defaults plus the repo's extensions.
func (c *Config) IgnoreGlobs() []string {
	return append(append([]string{}, DefaultIgnore...), c.Ignore...)
}
