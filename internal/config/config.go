// Package config loads and validates the consumer repo's .review-checker.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// LLM describes how to reach an OpenAI-compatible chat completions endpoint.
type LLM struct {
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	Model     string `yaml:"model"`

	// Temperature is omitted from the request when unset, so models that
	// reject the parameter (e.g. OpenAI o-series) use their default.
	Temperature *float32 `yaml:"temperature"`
}

// SuppressRule hides findings after the review runs — unlike ignore, the
// file is still reviewed; matching findings are just not posted. Path is a
// doublestar glob against the file path, Text a case-insensitive substring
// of the finding comment. When both are set, both must match.
type SuppressRule struct {
	Path string `yaml:"path"`
	Text string `yaml:"text"`
}

// Config is the parsed .review-checker.yaml.
type Config struct {
	LLM  LLM    `yaml:"llm"`
	Mode string `yaml:"mode"`

	// Ignore extends the built-in default ignore globs.
	Ignore []string `yaml:"ignore"`

	// Suppress drops findings matching any rule before posting.
	Suppress []SuppressRule `yaml:"suppress"`

	// CustomRules are repo-specific review rules appended to the generic
	// rules baked into the binary.
	CustomRules []string `yaml:"custom_rules"`

	// MaxFileTokens is the approximate per-file token budget before a
	// file's patch is truncated in the prompt.
	MaxFileTokens int `yaml:"max_file_tokens"`

	// MaxTotalTokens caps the whole diff section of the prompt; files past
	// the budget are omitted (and listed as omitted in the prompt).
	MaxTotalTokens int `yaml:"max_total_tokens"`
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

	defaultMaxFileTokens  = 8000
	defaultMaxTotalTokens = 60000
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
// Unknown fields are rejected so a typo (custom_rule: for custom_rules:)
// fails loudly instead of silently dropping the setting.
func Parse(raw []byte, path string) (*Config, error) {
	cfg := &Config{}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s is empty", path)
		}
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
	for i, r := range cfg.Suppress {
		if r.Path == "" && r.Text == "" {
			return nil, fmt.Errorf("%s: suppress[%d] needs path and/or text", path, i)
		}
		if r.Path != "" && !doublestar.ValidatePattern(r.Path) {
			return nil, fmt.Errorf("%s: suppress[%d] path %q is not a valid glob", path, i, r.Path)
		}
	}
	if cfg.MaxFileTokens <= 0 {
		cfg.MaxFileTokens = defaultMaxFileTokens
	}
	if cfg.MaxTotalTokens <= 0 {
		cfg.MaxTotalTokens = defaultMaxTotalTokens
	}

	return cfg, nil
}

// IgnoreGlobs returns the built-in defaults plus the repo's extensions.
func (c *Config) IgnoreGlobs() []string {
	return append(append([]string{}, DefaultIgnore...), c.Ignore...)
}
