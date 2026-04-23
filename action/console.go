package action

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/joeyciechanowicz/eve-bot/event"
)

func init() {
	Register("console", func(Deps) Handler { return Console{} })
}

// Console prints a compact JSON line for the event. Useful for local
// inspection; args are ignored.
type Console struct{}

func (Console) Execute(_ context.Context, ev *event.Event, _ map[string]any) error {
	payload := map[string]any{
		"id":          ev.ID,
		"source":      ev.Source,
		"type":        ev.Type,
		"occurred_at": ev.OccurredAt,
		"fields":      ev.Fields,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("console: marshal: %w", err)
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}
