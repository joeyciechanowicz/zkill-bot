// Package evescout is a Source that polls the EVE Scout public signatures
// API (https://api.eve-scout.com/v2/public/signatures) and emits one
// "signature.added" event per newly observed Thera/Turnur wormhole
// connection. Checkpoints are the most-recent signature created_at
// timestamp seen so far; on first run the baseline is "now" so the
// existing backlog is not replayed.
package evescout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/joeyciechanowicz/eve-bot/event"
	"github.com/joeyciechanowicz/eve-bot/source"
)

func init() {
	source.Register("evescout", func(name string, params map[string]any, deps source.Deps) (source.Source, error) {
		return New(Config{
			Name:         name,
			BaseURL:      stringParam(params, "base_url", "https://api.eve-scout.com"),
			Path:         stringParam(params, "path", "/v2/public/signatures"),
			PollInterval: durationParam(params, "poll_interval", 60*time.Second),
			UserAgent:    stringParam(params, "user_agent", ""),
		}, deps.HTTPClient, deps.Checkpointer), nil
	})
}

type Checkpointer interface {
	GetCheckpoint(source string) (string, bool)
	SetCheckpoint(source, value string) error
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

type Config struct {
	Name         string        // configured source name; used for checkpoints and event.Source
	BaseURL      string        // e.g. https://api.eve-scout.com
	Path         string        // e.g. /v2/public/signatures
	PollInterval time.Duration // between successful fetches (>=30s recommended)
	UserAgent    string
}

type Source struct {
	cfg    Config
	client *http.Client
	cp     Checkpointer
}

func New(cfg Config, client *http.Client, cp Checkpointer) *Source {
	if cfg.Name == "" {
		cfg.Name = "evescout"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.eve-scout.com"
	}
	if cfg.Path == "" {
		cfg.Path = "/v2/public/signatures"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 60 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "zkill-bot/2.0 (evescout)"
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Source{cfg: cfg, client: client, cp: cp}
}

func (s *Source) Name() string { return s.cfg.Name }

// signature mirrors a single item in the EVE Scout signatures response.
// Fields not used downstream are still decoded so they propagate verbatim
// into event.Fields for rule expressions.
type signature struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	CreatedByID     int64     `json:"created_by_id"`
	CreatedByName   string    `json:"created_by_name"`
	UpdatedAt       time.Time `json:"updated_at"`
	CompletedAt     time.Time `json:"completed_at"`
	Completed       bool      `json:"completed"`
	WhExitsOutward  bool      `json:"wh_exits_outward"`
	WhType          string    `json:"wh_type"`
	MaxShipSize     string    `json:"max_ship_size"`
	ExpiresAt       time.Time `json:"expires_at"`
	RemainingHours  float64   `json:"remaining_hours"`
	SignatureType   string    `json:"signature_type"`
	OutSystemID     int64     `json:"out_system_id"`
	OutSystemName   string    `json:"out_system_name"`
	OutSignature    string    `json:"out_signature"`
	InSystemID      int64     `json:"in_system_id"`
	InSystemClass   string    `json:"in_system_class"`
	InSystemName    string    `json:"in_system_name"`
	InRegionID      int64     `json:"in_region_id"`
	InRegionName    string    `json:"in_region_name"`
	InSignature     string    `json:"in_signature"`
}

func (s *Source) Run(ctx context.Context, out chan<- *event.Event) error {
	since := s.resolveCheckpoint()
	slog.Info("evescout: starting", "since", since)

	errBackoff := 5 * time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		sigs, err := s.fetch(ctx)
		if err != nil {
			slog.Warn("evescout: fetch failed, backing off", "error", err, "backoff", errBackoff)
			sleep(ctx, errBackoff)
			errBackoff = min(errBackoff*2, 5*time.Minute)
			continue
		}
		errBackoff = 5 * time.Second

		newest := since
		emitted := 0
		for i := range sigs {
			sig := &sigs[i]
			if !sig.CreatedAt.After(since) {
				continue
			}
			ev := toEvent(sig)
			ev.Source = s.cfg.Name
			select {
			case out <- ev:
				emitted++
			case <-ctx.Done():
				return nil
			}
			if sig.CreatedAt.After(newest) {
				newest = sig.CreatedAt
			}
		}
		if newest.After(since) {
			since = newest
			if err := s.cp.SetCheckpoint(s.Name(), since.UTC().Format(time.RFC3339Nano)); err != nil {
				slog.Warn("evescout: checkpoint save failed", "error", err)
			}
		}
		if emitted > 0 {
			slog.Debug("evescout: emitted signatures", "count", emitted, "since", since)
		}
		sleep(ctx, s.cfg.PollInterval)
	}
}

func (s *Source) resolveCheckpoint() time.Time {
	if v, ok := s.cp.GetCheckpoint(s.Name()); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
		slog.Warn("evescout: ignoring invalid checkpoint", "value", v)
	}
	return time.Now().UTC()
}

func (s *Source) fetch(ctx context.Context) ([]signature, error) {
	url := s.cfg.BaseURL + s.cfg.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var sigs []signature
	if err := json.NewDecoder(resp.Body).Decode(&sigs); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return sigs, nil
}

func toEvent(s *signature) *event.Event {
	return &event.Event{
		ID:         "evescout:" + s.ID,
		Source:     "evescout",
		Type:       "signature.added",
		OccurredAt: s.CreatedAt,
		Fields: map[string]any{
			"signature_id":     s.ID,
			"wh_type":          s.WhType,
			"max_ship_size":    s.MaxShipSize,
			"signature_type":   s.SignatureType,
			"wh_exits_outward": s.WhExitsOutward,
			"expires_at":       s.ExpiresAt,
			"remaining_hours":  s.RemainingHours,
			"created_at":       s.CreatedAt,
			"created_by_id":    s.CreatedByID,
			"created_by_name":  s.CreatedByName,
			"in": map[string]any{
				"system_id":    s.InSystemID,
				"system_name":  s.InSystemName,
				"system_class": s.InSystemClass,
				"region_id":    s.InRegionID,
				"region_name":  s.InRegionName,
				"signature":    s.InSignature,
			},
			"out": map[string]any{
				"system_id":   s.OutSystemID,
				"system_name": s.OutSystemName,
				"signature":   s.OutSignature,
			},
		},
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
