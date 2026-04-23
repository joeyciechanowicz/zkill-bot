package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/joeyciechanowicz/eve-bot/event"
)

func init() {
	Register("webhook", func(d Deps) Handler {
		c := d.HTTPClient
		if c == nil {
			c = http.DefaultClient
		}
		return Webhook{Client: c}
	})
}

// Webhook POSTs a JSON body to args["url"]. If args["body"] is provided it is
// sent verbatim (after templating); otherwise a default envelope with the
// event payload is used.
type Webhook struct {
	Client *http.Client
}

func (w Webhook) Execute(ctx context.Context, ev *event.Event, args map[string]any) error {
	url, _ := args["url"].(string)
	if url == "" {
		return fmt.Errorf("webhook: url is required")
	}

	var body []byte
	if b, ok := args["body"]; ok {
		switch v := b.(type) {
		case string:
			body = []byte(v)
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("webhook: marshal body: %w", err)
			}
			body = raw
		}
	} else {
		raw, err := json.Marshal(map[string]any{
			"id":          ev.ID,
			"source":      ev.Source,
			"type":        ev.Type,
			"occurred_at": ev.OccurredAt,
			"fields":      ev.Fields,
		})
		if err != nil {
			return fmt.Errorf("webhook: marshal default: %w", err)
		}
		body = raw
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.Client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook: HTTP %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}
