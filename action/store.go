package action

import (
	"context"
	"fmt"
	"time"

	"github.com/joeyciechanowicz/eve-bot/event"
)

func init() {
	Register("store", func(d Deps) Handler { return Store{W: d.FactWriter} })
}

// FactWriter is the subset of internal/store.Store that this action needs.
type FactWriter interface {
	Put(scope, key string, value any, ttl time.Duration) error
	Inc(scope, key, field string, by float64, ttl time.Duration) error
	Merge(scope, key string, delta map[string]any, ttl time.Duration) error
	Delete(scope, key string) error
}

// Store writes a fact to the shared store. Args:
//
//	scope: string (required)
//	key:   string (required)
//	op:    "set" | "inc" | "merge" | "delete"  (default "set")
//	value: any                                 (op=set)
//	field: string, by: number                  (op=inc)
//	value: map                                 (op=merge)
//	ttl:   duration string (e.g. "72h")        (default 0 = never)
type Store struct {
	W FactWriter
}

func (s Store) Execute(_ context.Context, _ *event.Event, args map[string]any) error {
	scope, _ := args["scope"].(string)
	key, _ := args["key"].(string)
	if scope == "" || key == "" {
		return fmt.Errorf("store: scope and key are required")
	}
	op, _ := args["op"].(string)
	if op == "" {
		op = "set"
	}

	var ttl time.Duration
	if t, ok := args["ttl"].(string); ok && t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("store: parse ttl %q: %w", t, err)
		}
		ttl = d
	}

	switch op {
	case "set":
		return s.W.Put(scope, key, args["value"], ttl)
	case "inc":
		field, _ := args["field"].(string)
		if field == "" {
			field = "count"
		}
		by := asFloat(args["by"], 1)
		return s.W.Inc(scope, key, field, by, ttl)
	case "merge":
		delta, ok := args["value"].(map[string]any)
		if !ok {
			return fmt.Errorf("store: merge requires value to be an object")
		}
		return s.W.Merge(scope, key, delta, ttl)
	case "delete":
		return s.W.Delete(scope, key)
	default:
		return fmt.Errorf("store: unknown op %q", op)
	}
}

func asFloat(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	default:
		return def
	}
}
