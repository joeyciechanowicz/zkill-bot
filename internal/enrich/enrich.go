// Package enrich defines the Enricher interface and a Chain that runs a list
// of enrichers in order. Source-specific enrichers (e.g. SDE lookups for
// zkill) live in the source package; cross-cutting enrichers live here.
package enrich

import (
	"context"
	"fmt"

	"github.com/joeyciechanowicz/eve-bot/event"
)

// Enricher mutates ev.Fields in place, adding enrichment data.
type Enricher interface {
	Enrich(ctx context.Context, ev *event.Event) error
}

// Chain runs a sequence of enrichers. If any returns an error, subsequent
// enrichers still run; errors are collected and returned together.
type Chain []Enricher

// Enrich runs every enricher. Errors are aggregated so one failure doesn't
// suppress later work.
func (c Chain) Enrich(ctx context.Context, ev *event.Event) error {
	var errs []error
	for _, e := range c {
		if err := e.Enrich(ctx, ev); err != nil {
			errs = append(errs, err)
		}
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("enrich: %d errors; first: %w", len(errs), errs[0])
	}
}
