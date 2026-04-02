package actions

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"zkill-bot/internal/killmail"
	"zkill-bot/internal/rules"
)

// Handler is the interface implemented by each action type.
type Handler interface {
	Execute(ctx context.Context, km *killmail.Killmail, args map[string]interface{}) error
}

// Counters tracks action execution statistics.
type Counters struct {
	Success int64
	Failure int64
	Retry   int64
}

// Dispatcher executes actions for matched rules with retry logic.
type Dispatcher struct {
	handlers    map[string]Handler
	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	Counters    Counters
}

// NewDispatcher constructs a Dispatcher wired to the given http client.
func NewDispatcher(client *http.Client, maxRetries int, baseBackoff, maxBackoff time.Duration) *Dispatcher {
	return &Dispatcher{
		handlers: map[string]Handler{
			"console": ConsoleAction{},
			"webhook": NewWebhookAction(client),
		},
		maxRetries:  maxRetries,
		baseBackoff: baseBackoff,
		maxBackoff:  maxBackoff,
	}
}

// Run executes all actions for the given rule matches against km.
func (d *Dispatcher) Run(ctx context.Context, km *killmail.Killmail, matches []rules.RuleMatch) {
	for _, m := range matches {
		for _, ac := range m.Actions {
			if err := d.executeWithRetry(ctx, km, ac); err != nil {
				slog.Error("action: failed after retries",
					"rule", m.Rule.Name,
					"action", ac.Type,
					"error", err,
				)
				d.Counters.Failure++
				continue
			}
			d.Counters.Success++
		}
	}
}

func (d *Dispatcher) executeWithRetry(ctx context.Context, km *killmail.Killmail, ac rules.ActionConfig) error {
	handler, ok := d.handlers[ac.Type]
	if !ok {
		return fmt.Errorf("unknown action type %q", ac.Type)
	}

	var lastErr error
	for attempt := 0; attempt <= d.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := d.backoffFor(attempt)
			slog.Debug("action: retrying", "action", ac.Type, "attempt", attempt, "backoff", backoff)
			d.Counters.Retry++
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("action: context cancelled during retry: %w", ctx.Err())
			}
		}

		if err := handler.Execute(ctx, km, ac.Args); err != nil {
			lastErr = err
			slog.Warn("action: attempt failed", "action", ac.Type, "attempt", attempt, "error", err)
			continue
		}
		return nil
	}
	return fmt.Errorf("action: all %d attempts failed: %w", d.maxRetries+1, lastErr)
}

func (d *Dispatcher) backoffFor(attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt-1))
	dur := time.Duration(float64(d.baseBackoff) * exp)
	if dur > d.maxBackoff {
		dur = d.maxBackoff
	}
	return dur
}
