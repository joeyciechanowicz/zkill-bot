package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// StatusResult represents the outcome of a single HTTP fetch attempt.
type StatusResult int

const (
	StatusOK      StatusResult = iota
	Status404                  // no killmail yet at this sequence
	Status429                  // rate limited
	Status403                  // blocked
	StatusError                // transient error
)

// FetchStats is emitted for observability after each fetch.
type FetchStats struct {
	Sequence int64
	Status   StatusResult
	Latency  time.Duration
}

// Poller continuously fetches killmails from the R2Z2 sequence API.
type Poller struct {
	baseURL      string
	seqPath      string
	pollInterval time.Duration
	backoff404   time.Duration
	client       *http.Client
	userAgent    string

	// Counters exposed for metrics collection.
	CountOK    int64
	Count404   int64
	Count429   int64
	Count403   int64
	CountError int64
}

// New creates a Poller with the given configuration.
func New(baseURL, seqPath string, pollInterval, backoff404 time.Duration) *Poller {
	return &Poller{
		baseURL:      baseURL,
		seqPath:      seqPath,
		pollInterval: pollInterval,
		backoff404:   backoff404,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		userAgent: "zkill-bot/1.0",
	}
}

// FetchStartSequence retrieves the current live sequence from sequence.json.
func (p *Poller) FetchStartSequence(ctx context.Context) (int64, error) {
	url := p.baseURL + p.seqPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("poller: build sequence request: %w", err)
	}
	req.Header.Set("User-Agent", p.userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("poller: fetch sequence.json: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("poller: sequence.json returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Sequence int64 `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("poller: decode sequence.json: %w", err)
	}
	if payload.Sequence == 0 {
		return 0, fmt.Errorf("poller: sequence.json returned sequence=0")
	}
	return payload.Sequence, nil
}

// Run polls the R2Z2 sequence stream starting at startSequence and sends raw
// JSON payloads on out. It runs until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, startSequence int64, out chan<- []byte) {
	seq := startSequence
	backoffDur := time.Second // for transient errors

	for {
		if ctx.Err() != nil {
			return
		}

		raw, status := p.fetchSequence(ctx, seq)

		switch status {
		case StatusOK:
			p.CountOK++
			backoffDur = time.Second // reset error backoff
			select {
			case out <- raw:
			case <-ctx.Done():
				return
			}
			seq++
			sleep(ctx, p.pollInterval)

		case Status404:
			p.Count404++
			slog.Debug("poller: 404, waiting for next killmail", "sequence", seq)
			sleep(ctx, p.backoff404)

		case Status429:
			p.Count429++
			slog.Warn("poller: rate limited (429), backing off", "sequence", seq)
			sleep(ctx, 10*time.Second)

		case Status403:
			p.Count403++
			slog.Error("poller: access blocked (403)", "sequence", seq)
			sleep(ctx, 60*time.Second)

		case StatusError:
			p.CountError++
			slog.Warn("poller: transient error, backing off", "sequence", seq, "backoff", backoffDur)
			sleep(ctx, backoffDur)
			backoffDur = min(backoffDur*2, 60*time.Second)
		}
	}
}

// fetchSequence fetches a single sequence file and returns raw bytes + status.
func (p *Poller) fetchSequence(ctx context.Context, seq int64) ([]byte, StatusResult) {
	url := fmt.Sprintf("%s/ephemeral/%d.json", p.baseURL, seq)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Error("poller: build request", "error", err)
		return nil, StatusError
	}
	req.Header.Set("User-Agent", p.userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, StatusError // context cancelled
		}
		slog.Error("poller: HTTP error", "sequence", seq, "error", err)
		return nil, StatusError
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("poller: read body", "sequence", seq, "error", err)
			return nil, StatusError
		}
		return body, StatusOK
	case http.StatusNotFound:
		return nil, Status404
	case http.StatusTooManyRequests:
		return nil, Status429
	case http.StatusForbidden:
		return nil, Status403
	default:
		slog.Warn("poller: unexpected status", "sequence", seq, "status", resp.StatusCode)
		return nil, StatusError
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
