package action

import (
	"context"
	"fmt"

	"zkill-bot/internal/event"
)

// ReplyPayload is the message shape sent on the event's _reply channel. The
// Discord source owns the channel and forwards whatever it receives back to
// the original interaction.
type ReplyPayload struct {
	Content   string
	Ephemeral bool
}

// Reply sends a ReplyPayload on event.Fields["_reply"] (expected to be a
// chan ReplyPayload). Used by Discord slash-command pipelines.
//
//	content:   string (required, templated)
//	ephemeral: bool   (default false)
type Reply struct{}

func (Reply) Execute(ctx context.Context, ev *event.Event, args map[string]any) error {
	ch, ok := ev.Fields["_reply"].(chan ReplyPayload)
	if !ok {
		return fmt.Errorf("reply: event has no _reply channel")
	}
	content, _ := args["content"].(string)
	if content == "" {
		return fmt.Errorf("reply: content is required")
	}
	ephemeral, _ := args["ephemeral"].(bool)

	select {
	case ch <- ReplyPayload{Content: content, Ephemeral: ephemeral}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
