// Package esi is a placeholder Source for EVE ESI-driven events (e.g.
// corporation asset changes, structure state). Not yet implemented.
package esi

import (
	"context"
	"fmt"

	"zkill-bot/internal/event"
)

type Config struct {
	BaseURL string
	Scope   string
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source { return &Source{cfg: cfg} }

func (s *Source) Name() string { return "esi" }

func (s *Source) Run(_ context.Context, _ chan<- *event.Event) error {
	return fmt.Errorf("esi: not implemented")
}
