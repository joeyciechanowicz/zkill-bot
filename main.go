package main

import (
	"context"
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
	// Signal-aware context for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Configuration ---
	cfg, err := config.Load()
	if err != nil {
		slog.Error("startup: config failed", "error", err)
		os.Exit(1)
	}

	if cfg.Debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	// --- Rules ---
	rf, err := rules.Load(cfg.RulesFilePath)
	if err != nil {
		slog.Error("startup: rules failed", "error", err)
		os.Exit(1)
	}
	slog.Info("rules loaded", "count", len(rf.Rules), "mode", rf.Mode)

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
	m.RunLogger(ctx, cfg.MetricsLogInterval, cfg.Debug)

	// --- Notifier ---
	notifier := metrics.NewNotifier(cfg.ObsAlertWebhookURL, httpClient)

	// --- Action dispatcher ---
	dispatcher := actions.NewDispatcher(
		st,
		httpClient,
		cfg.RetryMaxRetries,
		cfg.RetryBaseBackoff,
		cfg.RetryMaxBackoff,
	)

	// --- Determine start sequence ---
	p := poller.New(cfg.R2Z2BaseURL, cfg.R2Z2SequencePath, cfg.PollInterval, cfg.Poll404Backoff)

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

	processed := 0
	for {
		select {
		case raw, ok := <-rawCh:
			if !ok {
				goto shutdown
			}
			processKillmail(ctx, raw, enricher, rf, dispatcher, st, m, cfg)
			processed++
			// Prune action history every 1000 killmails to bound file size.
			if processed%1000 == 0 {
				st.PruneHistory(24 * time.Hour)
			}

		case <-ctx.Done():
			goto shutdown
		}
	}

shutdown:
	slog.Info("zkill-bot shutting down", "last_sequence", st.LastSequence)
	if err := st.Save(); err != nil {
		slog.Error("shutdown: save state failed", "error", err)
	}

	// Use a short background context for the shutdown notification since the
	// main context is already cancelled.
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
	// Normalize
	km, err := killmail.NormalizeFromR2Z2(raw)
	if err != nil {
		slog.Warn("pipeline: rejected malformed killmail", "error", err)
		m.KillmailsRejected.Add(1)
		return
	}

	// Enrich
	enricher.Enrich(km)

	// Evaluate rules
	matches := rules.Evaluate(km, rf)
	m.RuleMatches.Add(int64(len(matches)))

	// Execute actions
	if len(matches) > 0 {
		dispatcher.Run(ctx, km, matches)
		// Sync dispatcher counters into metrics
		m.ActionSuccess.Store(dispatcher.Counters.Success)
		m.ActionFailure.Store(dispatcher.Counters.Failure)
		m.ActionRetry.Store(dispatcher.Counters.Retry)
		m.ActionSkipDupe.Store(dispatcher.Counters.SkipDupe)
	}

	// Checkpoint: advance after successful processing
	st.LastSequence = km.SequenceID

	// Metrics
	m.KillmailsProcessed.Add(1)
	m.LastSequenceID.Store(km.SequenceID)
	m.LastProcessedAt.Store(time.Now().Unix())
	m.RecordLag(km.UploadedAt)

	// Persist after every killmail. Cheap because it's a small JSON file.
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
