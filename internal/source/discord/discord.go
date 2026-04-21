// Package discord is a placeholder Source that would turn Discord slash
// command interactions into events. Each incoming interaction gets a fresh
// `_reply` chan attached to Event.Fields; the `reply` action consumes it to
// send the response back to Discord.
//
// Not yet implemented.
package discord

import (
	"context"
	"fmt"

	"zkill-bot/internal/action"
	"zkill-bot/internal/event"
)

type Config struct {
	Token    string
	AppID    string
	GuildIDs []string
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source { return &Source{cfg: cfg} }

func (s *Source) Name() string { return "discord" }

// Run would connect to Discord, register slash commands, and for each
// interaction emit an event whose Fields include:
//
//	command:    string            (e.g. "evehistory")
//	user_id:    string
//	guild_id:   string
//	options:    map[string]any    (slash command option values)
//	_reply:     chan action.ReplyPayload
//
// The reply action is the only consumer of the _reply chan.
func (s *Source) Run(_ context.Context, _ chan<- *event.Event) error {
	_ = action.ReplyPayload{} // reference kept for clarity
	return fmt.Errorf("discord: not implemented")
}
