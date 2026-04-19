package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"zkill-bot/internal/rules"
)

// Config holds all runtime configuration loaded from a YAML file.
type Config struct {
	// State persistence
	StateFilePath string `yaml:"state_file_path"`

	// R2Z2 API
	R2Z2BaseURL      string `yaml:"r2z2_base_url"`
	R2Z2SequencePath string `yaml:"r2z2_sequence_path"`

	// Polling behaviour (milliseconds in YAML, time.Duration in Go)
	PollIntervalMS   int `yaml:"poll_interval_ms"`
	Poll404BackoffMS int `yaml:"poll_404_backoff_ms"`

	// Retry behaviour for actions
	RetryMaxRetries   int `yaml:"retry_max_retries"`
	RetryBaseBackoffMS int `yaml:"retry_base_backoff_ms"`
	RetryMaxBackoffMS  int `yaml:"retry_max_backoff_ms"`

	// Observability
	Debug                   bool   `yaml:"debug"`
	MetricsLogIntervalMS    int    `yaml:"metrics_log_interval_ms"`
	AlertWebhookURL         string `yaml:"alert_webhook_url"`
	ObsRepeated403Threshold int    `yaml:"obs_repeated_403_threshold"`
	ObsRepeated429Threshold int    `yaml:"obs_repeated_429_threshold"`

	// Rules — parsed inline from the same config file.
	Rules rules.RuleFile `yaml:"rules"`
}

// Derived duration accessors so callers don't multiply everywhere.
func (c *Config) PollInterval() time.Duration   { return ms(c.PollIntervalMS, 100) }
func (c *Config) Poll404Backoff() time.Duration  { return ms(c.Poll404BackoffMS, 6000) }
func (c *Config) RetryBaseBackoff() time.Duration { return ms(c.RetryBaseBackoffMS, 250) }
func (c *Config) RetryMaxBackoff() time.Duration  { return ms(c.RetryMaxBackoffMS, 10000) }
func (c *Config) MetricsLogInterval() time.Duration { return ms(c.MetricsLogIntervalMS, 60000) }

func ms(v, def int) time.Duration {
	if v <= 0 {
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(v) * time.Millisecond
}

// Load reads the YAML config file at path, applies defaults, and validates.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	applyDefaults(&c)

	if errs := validate(&c); len(errs) > 0 {
		return nil, errors.New("config: validation failed:\n  - " + strings.Join(errs, "\n  - "))
	}

	return &c, nil
}

func applyDefaults(c *Config) {
	if c.StateFilePath == "" {
		c.StateFilePath = "./state.json"
	}
	if c.R2Z2BaseURL == "" {
		c.R2Z2BaseURL = "https://r2z2.zkillboard.com"
	}
	if c.R2Z2SequencePath == "" {
		c.R2Z2SequencePath = "/ephemeral/sequence.json"
	}
	if c.RetryMaxRetries == 0 {
		c.RetryMaxRetries = 3
	}
	if c.ObsRepeated403Threshold == 0 {
		c.ObsRepeated403Threshold = 3
	}
	if c.ObsRepeated429Threshold == 0 {
		c.ObsRepeated429Threshold = 5
	}
	if c.Rules.Mode == "" {
		c.Rules.Mode = rules.ModeFirstMatch
	}
}

// Watch polls path for modification time changes every interval and calls
// onChange with the newly loaded config. Invalid reloads are logged and
// skipped — the previous config remains active. Runs until ctx is cancelled.
//
// Fields that are wired into already-running goroutines at startup
// (poll_interval_ms, poll_404_backoff_ms, retry_*, state_file_path,
// r2z2_base_url, metrics_log_interval_ms) are read from the new config
// object but have no effect on those goroutines until the next restart.
func Watch(ctx context.Context, path string, interval time.Duration, onChange func(*Config)) {
	info, err := os.Stat(path)
	if err != nil {
		slog.Warn("config: watch: initial stat failed", "path", path, "error", err)
		return
	}
	lastMod := info.ModTime()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			info, err := os.Stat(path)
			if err != nil {
				slog.Warn("config: watch: stat failed", "path", path, "error", err)
				continue
			}
			if !info.ModTime().After(lastMod) {
				continue
			}
			lastMod = info.ModTime()
			cfg, err := Load(path)
			if err != nil {
				slog.Warn("config: hot-reload failed, keeping previous config", "error", err)
				continue
			}
			slog.Info("config: hot-reloaded", "rules", len(cfg.Rules.Rules), "mode", cfg.Rules.Mode)
			onChange(cfg)
		case <-ctx.Done():
			return
		}
	}
}

func validate(c *Config) []string {
	var errs []string

	if _, err := url.ParseRequestURI(c.R2Z2BaseURL); err != nil {
		errs = append(errs, fmt.Sprintf("r2z2_base_url=%q: invalid URL", c.R2Z2BaseURL))
	}
	if c.AlertWebhookURL != "" {
		if _, err := url.ParseRequestURI(c.AlertWebhookURL); err != nil {
			errs = append(errs, fmt.Sprintf("alert_webhook_url=%q: invalid URL", c.AlertWebhookURL))
		}
	}
	if c.Rules.Mode != rules.ModeFirstMatch && c.Rules.Mode != rules.ModeMultiMatch {
		errs = append(errs, fmt.Sprintf("rules.mode=%q: must be %q or %q", c.Rules.Mode, rules.ModeFirstMatch, rules.ModeMultiMatch))
	}
	if len(c.Rules.Rules) == 0 {
		errs = append(errs, "rules.rules: no rules defined")
	}

	return errs
}
