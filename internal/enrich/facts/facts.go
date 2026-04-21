// Package facts provides an enricher that loads pre-configured facts from the
// store into event.Fields so they can be consumed by rule expressions and
// action templates without per-rule fact() lookups.
package facts

import (
	"context"
	"fmt"

	"zkill-bot/internal/event"
)

// Reader is the subset of store.Store needed for fact enrichment.
type Reader interface {
	GetAny(scope, key string) any
}

// Lookup describes a single fact to load. Key is a dotted-path into ev.Fields
// whose value becomes the lookup key (e.g. KeyPath="victim.character_id").
// The loaded value is placed under `facts.<Scope>` in the event.
type Lookup struct {
	Scope   string
	KeyPath string
	Alias   string // optional; defaults to Scope
}

// Enricher loads each configured Lookup from the store into ev.Fields["facts"].
type Enricher struct {
	R       Reader
	Lookups []Lookup
}

func New(r Reader, lookups []Lookup) *Enricher {
	return &Enricher{R: r, Lookups: lookups}
}

func (e *Enricher) Enrich(_ context.Context, ev *event.Event) error {
	if ev.Fields == nil {
		ev.Fields = map[string]any{}
	}
	bag, _ := ev.Fields["facts"].(map[string]any)
	if bag == nil {
		bag = map[string]any{}
	}
	for _, lu := range e.Lookups {
		raw := ev.Get(lu.KeyPath)
		if raw == nil {
			continue
		}
		key := fmt.Sprintf("%v", raw)
		alias := lu.Alias
		if alias == "" {
			alias = lu.Scope
		}
		if v := e.R.GetAny(lu.Scope, key); v != nil {
			bag[alias] = v
		}
	}
	ev.Fields["facts"] = bag
	return nil
}
