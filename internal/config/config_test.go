package config

import (
	"strings"
	"testing"
)

const validYAML = `
llm:
  base_url: https://api.deepseek.com/v1
  api_key_env: DEEPSEEK_API_KEY
  model: deepseek-chat
`

func TestParseValidWithDefaults(t *testing.T) {
	cfg, err := Parse([]byte(validYAML), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeCommentOnly {
		t.Errorf("mode: got %q, want default %q", cfg.Mode, ModeCommentOnly)
	}
	if cfg.MaxFileTokens != defaultMaxFileTokens {
		t.Errorf("max_file_tokens: got %d, want default %d", cfg.MaxFileTokens, defaultMaxFileTokens)
	}
	if cfg.MaxTotalTokens != defaultMaxTotalTokens {
		t.Errorf("max_total_tokens: got %d, want default %d", cfg.MaxTotalTokens, defaultMaxTotalTokens)
	}
	if cfg.LLM.Temperature != nil {
		t.Errorf("temperature: got %v, want nil (omitted)", *cfg.LLM.Temperature)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	yaml := validYAML + "custom_rule:\n  - typo, should be custom_rules\n"
	_, err := Parse([]byte(yaml), "test.yaml")
	if err == nil {
		t.Fatal("want error for unknown field custom_rule (typo)")
	}
	if !strings.Contains(err.Error(), "custom_rule") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}

func TestParseRejectsMissingRequired(t *testing.T) {
	for _, missing := range []string{"base_url", "api_key_env", "model"} {
		yaml := strings.Replace(validYAML, missing, "x_"+missing, 1)
		if _, err := Parse([]byte(yaml), "test.yaml"); err == nil {
			t.Errorf("want error when llm.%s is missing", missing)
		}
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(""), "test.yaml"); err == nil {
		t.Error("want error for empty config")
	}
}

func TestParseRejectsUnsupportedMode(t *testing.T) {
	if _, err := Parse([]byte(validYAML+"mode: blocking\n"), "test.yaml"); err == nil {
		t.Error("want error for mode: blocking in v1")
	}
}

func TestIgnoreGlobsExtendsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(validYAML+"ignore:\n  - \"docs/**\"\n"), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	globs := cfg.IgnoreGlobs()
	if len(globs) != len(DefaultIgnore)+1 {
		t.Fatalf("got %d globs, want defaults(%d)+1", len(globs), len(DefaultIgnore))
	}
	if globs[len(globs)-1] != "docs/**" {
		t.Errorf("repo glob missing: %v", globs)
	}
	if cfg.LLM.Temperature != nil {
		t.Error("temperature should stay nil unless set")
	}
}

func TestParseTemperature(t *testing.T) {
	cfg, err := Parse([]byte(validYAML+"  # placeholder\n"), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg

	withTemp := strings.Replace(validYAML, "model: deepseek-chat", "model: deepseek-chat\n  temperature: 0.2", 1)
	cfg, err = Parse([]byte(withTemp), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Temperature == nil || *cfg.LLM.Temperature != 0.2 {
		t.Errorf("temperature: got %v, want 0.2", cfg.LLM.Temperature)
	}
}
