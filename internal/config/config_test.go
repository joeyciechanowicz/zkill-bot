package config_test

import (
	"os"
	"testing"

	"zkill-bot/internal/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, `
eve_db_path: ../../eve.db
rules:
  mode: first-match
  rules:
    - name: test
      enabled: true
      priority: 1
      filter:
        solo: true
      actions:
        - type: console
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Rules.Mode != "first-match" {
		t.Errorf("Rules.Mode: got %q", cfg.Rules.Mode)
	}
	if len(cfg.Rules.Rules) != 1 {
		t.Errorf("Rules.Rules: got %d, want 1", len(cfg.Rules.Rules))
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeConfig(t, `
eve_db_path: ../../eve.db
rules:
  rules:
    - name: test
      enabled: true
      priority: 1
      filter:
        solo: true
      actions:
        - type: console
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.R2Z2BaseURL != "https://r2z2.zkillboard.com" {
		t.Errorf("R2Z2BaseURL default: got %q", cfg.R2Z2BaseURL)
	}
	if cfg.RetryMaxRetries != 3 {
		t.Errorf("RetryMaxRetries default: got %d", cfg.RetryMaxRetries)
	}
	if cfg.Rules.Mode != "first-match" {
		t.Errorf("Rules.Mode default: got %q", cfg.Rules.Mode)
	}
	if cfg.PollInterval().Milliseconds() != 100 {
		t.Errorf("PollInterval default: got %v", cfg.PollInterval())
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeConfig(t, `{not: valid: yaml`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_NoRules(t *testing.T) {
	path := writeConfig(t, `
eve_db_path: ../../eve.db
rules:
  mode: first-match
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error when no rules defined")
	}
}

func TestLoad_InvalidWebhookURL(t *testing.T) {
	path := writeConfig(t, `
eve_db_path: ../../eve.db
alert_webhook_url: "not a url"
rules:
  rules:
    - name: test
      enabled: true
      priority: 1
      filter:
        solo: true
      actions:
        - type: console
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for invalid alert_webhook_url")
	}
}
