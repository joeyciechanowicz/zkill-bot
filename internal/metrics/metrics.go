package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics holds all runtime counters for the bot.
// All fields are safe for concurrent access via atomic operations.
type Metrics struct {
	FetchOK    atomic.Int64
	Fetch404   atomic.Int64
	Fetch429   atomic.Int64
	Fetch403   atomic.Int64
	FetchError atomic.Int64

	KillmailsProcessed atomic.Int64
	KillmailsRejected  atomic.Int64

	RuleMatches atomic.Int64

	ActionSuccess atomic.Int64
	ActionFailure atomic.Int64
	ActionRetry   atomic.Int64

	LastProcessedAt atomic.Int64 // unix timestamp
	LastSequenceID  atomic.Int64
	LastLagSeconds  atomic.Int64 // seconds between killmail_time and processing
}

// RecordLag records the lag between killmail upload time and now.
func (m *Metrics) RecordLag(uploadedAt time.Time) {
	lag := int64(time.Since(uploadedAt).Seconds())
	m.LastLagSeconds.Store(lag)
}

// Log emits a structured log line with all current counter values.
func (m *Metrics) Log() {
	slog.Info("metrics",
		"fetch_ok", m.FetchOK.Load(),
		"fetch_404", m.Fetch404.Load(),
		"fetch_429", m.Fetch429.Load(),
		"fetch_403", m.Fetch403.Load(),
		"fetch_error", m.FetchError.Load(),
		"killmails_processed", m.KillmailsProcessed.Load(),
		"killmails_rejected", m.KillmailsRejected.Load(),
		"rule_matches", m.RuleMatches.Load(),
		"action_success", m.ActionSuccess.Load(),
		"action_failure", m.ActionFailure.Load(),
		"action_retry", m.ActionRetry.Load(),
		"last_sequence_id", m.LastSequenceID.Load(),
		"last_lag_seconds", m.LastLagSeconds.Load(),
	)
}

// RunLogger periodically calls Log on the given interval until ctx is done.
// Only runs if debug is true.
func (m *Metrics) RunLogger(ctx context.Context, interval time.Duration, debug bool) {
	if !debug {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				m.Log()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Notifier sends startup and shutdown messages to a Discord webhook.
type Notifier struct {
	webhookURL string
	client     *http.Client
}

// NewNotifier creates a Notifier. If webhookURL is empty, all calls are no-ops.
func NewNotifier(webhookURL string, client *http.Client) *Notifier {
	return &Notifier{webhookURL: webhookURL, client: client}
}

// NotifyStartup sends a startup message with the starting sequence.
func (n *Notifier) NotifyStartup(ctx context.Context, startSequence int64) {
	if n.webhookURL == "" {
		return
	}
	n.send(ctx, fmt.Sprintf("zkill-bot started. Polling from sequence **%d**.", startSequence))
}

// NotifyShutdown sends a shutdown message with the last processed sequence.
func (n *Notifier) NotifyShutdown(ctx context.Context, lastSequence int64) {
	if n.webhookURL == "" {
		return
	}
	n.send(ctx, fmt.Sprintf("zkill-bot stopped. Last processed sequence: **%d**.", lastSequence))
}

func (n *Notifier) send(ctx context.Context, content string) {
	payload := struct {
		Content string `json:"content"`
	}{Content: content}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("notifier: marshal payload", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("notifier: build request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zkill-bot/1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("notifier: send", "error", err)
		return
	}
	resp.Body.Close()
}
