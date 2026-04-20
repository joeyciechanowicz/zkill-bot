package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"zkill-bot/internal/actions"
	"zkill-bot/internal/config"
	"zkill-bot/internal/enrichment"
	"zkill-bot/internal/evescout"
	"zkill-bot/internal/killmail"
	"zkill-bot/internal/metrics"
	"zkill-bot/internal/poller"
	"zkill-bot/internal/rules"
	"zkill-bot/internal/state"
)

func main() {
	configPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	// Signal-aware context for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Configuration (includes rules) ---
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("startup: config failed", "error", err)
		os.Exit(1)
	}

	if cfg.Debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	slog.Info("rules loaded", "count", len(cfg.Rules.Rules), "mode", cfg.Rules.Mode)

	// liveCfg is swapped atomically by the config watcher goroutine.
	var liveCfg atomic.Pointer[config.Config]
	liveCfg.Store(cfg)
	go config.Watch(ctx, *configPath, 5*time.Second, func(newCfg *config.Config) {
		liveCfg.Store(newCfg)
	})

	// --- Enrichment ---
	enricher := enrichment.New()

	// --- State ---
	st, err := state.Load(cfg.StateFilePath)
	if err != nil {
		slog.Error("startup: state load failed", "error", err)
		os.Exit(1)
	}

	// --- Shared HTTP client ---
	httpClient := &http.Client{Timeout: 15 * time.Second}

	// --- Metrics ---
	m := &metrics.Metrics{}
	m.RunLogger(ctx, cfg.MetricsLogInterval(), cfg.Debug)

	// --- Notifier ---
	notifier := metrics.NewNotifier(cfg.AlertWebhookURL, httpClient)

	// --- Eve Scout wormhole client ---
	esClient := evescout.New(httpClient)

	// --- Action dispatcher ---
	dispatcher := actions.NewDispatcher(
		httpClient,
		cfg.RetryMaxRetries,
		cfg.RetryBaseBackoff(),
		cfg.RetryMaxBackoff(),
	)

	// --- Determine start sequence ---
	p := poller.New(cfg.R2Z2BaseURL, cfg.R2Z2SequencePath, cfg.PollInterval(), cfg.Poll404Backoff())

	startSeq := st.LastSequence
	if startSeq > 0 {
		startSeq++ // resume from next after last processed
		slog.Info("resuming from checkpoint", "sequence", startSeq)
	} else {
		startSeq, err = p.FetchStartSequence(ctx)
		if err != nil {
			slog.Error("startup: fetch start sequence failed", "error", err)
			os.Exit(1)
		}
		slog.Info("starting from live sequence", "sequence", startSeq)
	}

	// --- Startup notification ---
	notifier.NotifyStartup(ctx, startSeq)

	// --- Eve Scout startup check ---
	// Alert if any watched solar systems (from solar_system_name filters) have
	// active Thera/Turnur wormhole connections right now.
	checkWormholesAtStartup(ctx, esClient, &cfg.Rules, notifier)

	// --- Poll loop ---
	rawCh := make(chan []byte, 32)
	go p.Run(ctx, startSeq, rawCh)

	for {
		select {
		case raw, ok := <-rawCh:
			if !ok {
				goto shutdown
			}
			current := liveCfg.Load()
			processKillmail(ctx, raw, enricher, esClient, &current.Rules, dispatcher, st, m, current)

		case <-ctx.Done():
			goto shutdown
		}
	}

shutdown:
	slog.Info("zkill-bot shutting down", "last_sequence", st.LastSequence)
	if err := st.Save(); err != nil {
		slog.Error("shutdown: save state failed", "error", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	notifier.NotifyShutdown(shutCtx, st.LastSequence)
	slog.Info("zkill-bot stopped")
}

func checkWormholesAtStartup(ctx context.Context, es *evescout.Client, rf *rules.RuleFile, notifier *metrics.Notifier) {
	watchedSystems := rules.ExtractSolarSystemNames(rf)
	if len(watchedSystems) == 0 {
		return
	}

	sigs, err := es.FetchAll()
	if err != nil {
		slog.Warn("startup: evescout fetch failed", "error", err)
		return
	}

	// Index watched systems for O(1) lookup.
	watched := make(map[string]bool, len(watchedSystems))
	for _, name := range watchedSystems {
		watched[strings.ToLower(name)] = true
	}

	var lines []string
	for _, sig := range sigs {
		if watched[strings.ToLower(sig.InSystemName)] {
			lines = append(lines, fmt.Sprintf("**%s** → %s (type: %s, max ship: %s)",
				sig.InSystemName, sig.OutSystemName, sig.WHType, sig.MaxShipSize))
		}
	}

	if len(lines) == 0 {
		return
	}

	msg := "Wormhole alert: watched systems with active Thera/Turnur connections:\n" +
		strings.Join(lines, "\n")
	slog.Info("startup: wormhole connections found for watched systems", "count", len(lines))
	notifier.Notify(ctx, msg)
}

func processKillmail(
	ctx context.Context,
	raw []byte,
	enricher *enrichment.Enricher,
	es *evescout.Client,
	rf *rules.RuleFile,
	dispatcher *actions.Dispatcher,
	st *state.State,
	m *metrics.Metrics,
	cfg *config.Config,
) {
	km, err := killmail.NormalizeFromR2Z2(raw)
	if err != nil {
		slog.Warn("pipeline: rejected malformed killmail", "error", err)
		m.KillmailsRejected.Add(1)
		return
	}

	enricher.Enrich(km)

	if km.Enriched != nil && km.Enriched.SolarSystemName != "" {
		if conns, err := es.Lookup(km.Enriched.SolarSystemName); err != nil {
			slog.Warn("evescout: lookup failed", "system", km.Enriched.SolarSystemName, "error", err)
		} else {
			km.Enriched.WormholeConnections = conns
		}
	}

	matches := rules.Evaluate(km, rf)
	m.RuleMatches.Add(int64(len(matches)))

	if len(matches) > 0 {
		dispatcher.Run(ctx, km, matches)
		m.ActionSuccess.Store(dispatcher.Counters.Success)
		m.ActionFailure.Store(dispatcher.Counters.Failure)
		m.ActionRetry.Store(dispatcher.Counters.Retry)
	}

	st.LastSequence = km.SequenceID
	m.KillmailsProcessed.Add(1)
	m.LastSequenceID.Store(km.SequenceID)
	m.LastProcessedAt.Store(time.Now().Unix())
	m.RecordLag(km.UploadedAt)

	if err := st.Save(); err != nil {
		slog.Error("pipeline: save state", "error", err)
	}

	if cfg.Debug {
		slog.Debug("pipeline: processed",
			"killmail_id", km.KillmailID,
			"sequence", km.SequenceID,
			"ship", km.Enriched.VictimShipName,
			"value", km.ZKB.TotalValue,
			"rules_matched", len(matches),
			"lag_s", m.LastLagSeconds.Load(),
		)
	}
}
