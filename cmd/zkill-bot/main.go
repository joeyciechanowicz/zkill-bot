// zkill-bot runs a shared rule engine fed by one or more configured sources.
// Each source produces events into a shared channel that a single processor
// evaluates against the compiled rule set and dispatches matched actions.
// All sources share one SQLite fact/checkpoint store.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"zkill-bot/internal/action"
	"zkill-bot/internal/pipeline"
	"zkill-bot/internal/rules"
	"zkill-bot/internal/source"
	"zkill-bot/internal/source/evescout"
	"zkill-bot/internal/source/zkill"
	"zkill-bot/internal/store"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		slog.Error("startup: config", "error", err)
		os.Exit(1)
	}

	if cfg.Debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		slog.Error("startup: store", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	go st.RunJanitor(ctx, cfg.Store.JanitorInterval, cfg.Store.ActionHistoryTTL)

	httpClient := &http.Client{Timeout: 15 * time.Second}

	p, err := buildPipeline(cfg, st, httpClient)
	if err != nil {
		slog.Error("startup: build pipeline", "error", err)
		os.Exit(1)
	}
	slog.Info("zkill-bot starting", "sources", len(p.Sources), "rules", len(cfg.Rules.Rules))

	if err := p.Run(ctx); err != nil {
		slog.Error("runtime", "error", err)
		os.Exit(1)
	}
	slog.Info("zkill-bot stopped")
}

func buildPipeline(cfg *Config, st *store.Store, hc *http.Client) (*pipeline.Pipeline, error) {
	handlers := map[string]action.Handler{
		"console": action.Console{},
		"webhook": action.Webhook{Client: hc},
		"store":   action.Store{W: st},
		"reply":   action.Reply{},
	}

	srcs := make([]source.Source, 0, len(cfg.Sources))
	for _, sc := range cfg.Sources {
		src, err := buildSource(sc, st, hc)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", sc.Name, err)
		}
		srcs = append(srcs, src)
	}

	ruleSet, err := rules.Compile(cfg.Rules.Mode, cfg.Rules.Rules)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}

	disp := action.New(handlers, st, cfg.Retry.MaxRetries, cfg.Retry.BaseBackoff, cfg.Retry.MaxBackoff)

	return &pipeline.Pipeline{
		Sources:    srcs,
		Rules:      ruleSet,
		Dispatcher: disp,
		Facts:      st,
		BufferSize: cfg.BufferSize,
	}, nil
}

func buildSource(sc SourceConfig, st *store.Store, hc *http.Client) (source.Source, error) {
	switch sc.Type {
	case "zkill":
		return zkill.New(zkill.Config{
			Name:         sc.Name,
			BaseURL:      stringParam(sc.Params, "base_url", "https://r2z2.zkillboard.com"),
			SequencePath: stringParam(sc.Params, "sequence_path", "/ephemeral/sequence.json"),
			PollInterval: durationParam(sc.Params, "poll_interval", 100*time.Millisecond),
			Backoff404:   durationParam(sc.Params, "backoff_404", 6*time.Second),
		}, hc, st), nil
	case "evescout":
		return evescout.New(evescout.Config{
			Name:         sc.Name,
			BaseURL:      stringParam(sc.Params, "base_url", "https://api.eve-scout.com"),
			Path:         stringParam(sc.Params, "path", "/v2/public/signatures"),
			PollInterval: durationParam(sc.Params, "poll_interval", 60*time.Second),
			UserAgent:    stringParam(sc.Params, "user_agent", ""),
		}, hc, st), nil
	default:
		return nil, fmt.Errorf("unknown source type %q", sc.Type)
	}
}

func stringParam(m map[string]any, k, def string) string {
	if v, ok := m[k].(string); ok && v != "" {
		return v
	}
	return def
}

func durationParam(m map[string]any, k string, def time.Duration) time.Duration {
	switch v := m[k].(type) {
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	case int:
		return time.Duration(v) * time.Millisecond
	}
	return def
}
