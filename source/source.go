// Package source defines the interface a pipeline datasource must implement.
// Concrete sources live in subpackages (zkill, whapi, esi, discord).
package source

import (
	"context"

	"github.com/joeyciechanowicz/eve-bot/event"
)

// Source produces Events onto out until ctx is cancelled. Sources own their
// own checkpoint persistence via the store they were constructed with;
// malformed payloads should be dropped with a warning, not returned.
type Source interface {
	// Name is a stable identifier used for checkpoints and logs.
	Name() string
	// Run blocks until ctx is cancelled. It must close nothing; the caller
	// owns the out channel.
	Run(ctx context.Context, out chan<- *event.Event) error
}
