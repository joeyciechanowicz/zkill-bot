// Package whapi is a placeholder Source for a wormhole-connections API. It
// demonstrates the shape a new source would take; the actual HTTP polling
// and event normalization are not yet implemented.
package whapi

import (
	"context"
	"fmt"

	"zkill-bot/internal/event"
)

type Config struct {
	BaseURL string
	APIKey  string
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source { return &Source{cfg: cfg} }

func (s *Source) Name() string { return "whapi" }

// Run is intentionally unimplemented.
func (s *Source) Run(_ context.Context, _ chan<- *event.Event) error {
	return fmt.Errorf("whapi: not implemented")
}
