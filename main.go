package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"zkill-bot/internal/actions"
	"zkill-bot/internal/config"
	"zkill-bot/internal/enrichment"
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

	// --- Enrichment ---
	enricher, err := enrichment.New(cfg.EVEDBPath)
	if err != nil {
		slog.Error("startup: enrichment db failed", "error", err)
		os.Exit(1)
	}
	defer enricher.Close()

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

	// --- Poll loop ---
	rawCh := make(chan []byte, 32)
	go p.Run(ctx, startSeq, rawCh)

	for {
		select {
		case raw, ok := <-rawCh:
			if !ok {
				goto shutdown
			}
			processKillmail(ctx, raw, enricher, &cfg.Rules, dispatcher, st, m, cfg)

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

func processKillmail(
	ctx context.Context,
	raw []byte,
	enricher *enrichment.Enricher,
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
