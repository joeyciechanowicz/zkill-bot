// Package zkill is a Source that polls the zKillboard R2Z2 sequence stream,
// normalizes each killmail into an event.Event, and enriches it inline using
// the SDE static data. Checkpoints are the sequence id.
package zkill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"zkill-bot/internal/event"
)

// Checkpointer is the subset of store.Store that the zkill source needs.
type Checkpointer interface {
	GetCheckpoint(source string) (string, bool)
	SetCheckpoint(source, value string) error
}

// Config drives the poller.
type Config struct {
	Name         string        // configured source name; used for checkpoints and event.Source
	BaseURL      string        // e.g. https://r2z2.zkillboard.com
	SequencePath string        // e.g. /ephemeral/sequence.json
	PollInterval time.Duration // between successful fetches (>=100ms)
	Backoff404   time.Duration // between 404 retries (>=6s recommended)
	UserAgent    string
}

// Source polls zKillboard and emits killmail events.
type Source struct {
	cfg    Config
	client *http.Client
	cp     Checkpointer
}

// New builds a zkill source. The store is used for sequence checkpointing.
func New(cfg Config, client *http.Client, cp Checkpointer) *Source {
	if cfg.Name == "" {
		cfg.Name = "zkill"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "zkill-bot/2.0"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	if cfg.Backoff404 <= 0 {
		cfg.Backoff404 = 6 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Source{cfg: cfg, client: client, cp: cp}
}

// Name returns the configured identifier for this source instance.
func (s *Source) Name() string { return s.cfg.Name }

// Run polls R2Z2 and emits events. Returns when ctx is cancelled.
func (s *Source) Run(ctx context.Context, out chan<- *event.Event) error {
	seq, err := s.resolveStartSequence(ctx)
	if err != nil {
		return fmt.Errorf("zkill: resolve start sequence: %w", err)
	}
	slog.Info("zkill: starting", "sequence", seq)

	errBackoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		raw, status := s.fetch(ctx, seq)
		switch status {
		case statusOK:
			errBackoff = time.Second
			ev, err := normalize(raw)
			if err != nil {
				slog.Warn("zkill: rejected malformed payload", "sequence", seq, "error", err)
			} else {
				ev.Source = s.cfg.Name
				enrich(ev)
				select {
				case out <- ev:
				case <-ctx.Done():
					return nil
				}
				if err := s.cp.SetCheckpoint(s.Name(), strconv.FormatInt(seq, 10)); err != nil {
					slog.Warn("zkill: checkpoint save failed", "error", err)
				}
			}
			seq++
			sleep(ctx, s.cfg.PollInterval)

		case status404:
			slog.Debug("zkill: 404, waiting", "sequence", seq)
			sleep(ctx, s.cfg.Backoff404)

		case status429:
			slog.Warn("zkill: rate limited", "sequence", seq)
			sleep(ctx, 10*time.Second)

		case status403:
			slog.Error("zkill: access blocked (403)", "sequence", seq)
			sleep(ctx, 60*time.Second)

		case statusError:
			slog.Warn("zkill: transient error, backing off", "sequence", seq, "backoff", errBackoff)
			sleep(ctx, errBackoff)
			errBackoff = min(errBackoff*2, 60*time.Second)
		}
	}
}

func (s *Source) resolveStartSequence(ctx context.Context) (int64, error) {
	if v, ok := s.cp.GetCheckpoint(s.Name()); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil && n > 0 {
			return n + 1, nil
		}
		slog.Warn("zkill: ignoring invalid checkpoint", "value", v)
	}
	return s.fetchLiveSequence(ctx)
}

func (s *Source) fetchLiveSequence(ctx context.Context) (int64, error) {
	url := s.cfg.BaseURL + s.cfg.SequencePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("sequence.json HTTP %d", resp.StatusCode)
	}
	var body struct {
		Sequence int64 `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.Sequence == 0 {
		return 0, fmt.Errorf("sequence.json returned 0")
	}
	return body.Sequence, nil
}

type fetchStatus int

const (
	statusOK fetchStatus = iota
	status404
	status429
	status403
	statusError
)

func (s *Source) fetch(ctx context.Context, seq int64) ([]byte, fetchStatus) {
	url := fmt.Sprintf("%s/ephemeral/%d.json", s.cfg.BaseURL, seq)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, statusError
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, statusError
		}
		slog.Error("zkill: http error", "sequence", seq, "error", err)
		return nil, statusError
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, statusError
		}
		return body, statusOK
	case http.StatusNotFound:
		return nil, status404
	case http.StatusTooManyRequests:
		return nil, status429
	case http.StatusForbidden:
		return nil, status403
	default:
		slog.Warn("zkill: unexpected status", "sequence", seq, "status", resp.StatusCode)
		return nil, statusError
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
