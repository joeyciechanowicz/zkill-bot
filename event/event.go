// Package event defines the canonical event shape that flows through every
// pipeline. A Source produces Events; Enrichers mutate Fields; Rules evaluate
// expressions over Fields; Actions consume Events to produce side effects.
package event

import (
	"strings"
	"time"
)

// Event is the unit of work in a pipeline. Sources populate it; everything
// downstream reads (and may mutate) Fields.
type Event struct {
	// ID is a stable, source-unique identifier used for action idempotency.
	// Convention: "<source>:<natural-id>", e.g. "zkill:134435757".
	ID string

	// Source identifies the producer ("zkill", "whapi", "esi", "discord").
	Source string

	// Type is a source-defined event type ("killmail", "connection.added", ...).
	Type string

	// OccurredAt is the original event time as reported by the source.
	OccurredAt time.Time

	// Fields holds the structured payload. Keys are dotted-path addressable
	// from rule expressions; nested values should be map[string]any or slices
	// of such. Values must be JSON-marshalable for templating.
	Fields map[string]any
}

// Get resolves a dotted path against Fields and returns the value (or nil).
// e.g. Get("zkb.total_value") or Get("victim.character_id"). Slices are not
// indexable through this helper; use it for scalar/object access.
func (e *Event) Get(path string) any {
	if e == nil || e.Fields == nil || path == "" {
		return nil
	}
	var cur any = e.Fields
	for p := range strings.SplitSeq(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}
