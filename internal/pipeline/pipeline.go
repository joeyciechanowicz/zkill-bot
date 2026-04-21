// Package pipeline wires a set of Sources to a shared compiled Rule set and
// action Dispatcher. One goroutine per source feeds events into a shared
// channel; a single processor goroutine evaluates rules and dispatches.
package pipeline

import (
	"context"
	"log/slog"
	"sync"

	"zkill-bot/internal/action"
	"zkill-bot/internal/event"
	"zkill-bot/internal/rules"
	"zkill-bot/internal/source"
)

// Pipeline runs every configured source concurrently and funnels their events
// through a single shared rule engine.
type Pipeline struct {
	Sources    []source.Source
	Rules      *rules.Set
	Dispatcher *action.Dispatcher
	Facts      rules.FactStore // passed to rule expressions
	BufferSize int             // shared channel buffer
}

// Run starts every source, processes events until ctx is done, and returns
// once all source goroutines have exited.
func (p *Pipeline) Run(ctx context.Context) error {
	buf := p.BufferSize
	if buf <= 0 {
		buf = 32 * max(len(p.Sources), 1)
	}
	ch := make(chan *event.Event, buf)

	var wg sync.WaitGroup
	for _, src := range p.Sources {
		wg.Add(1)
		go func(src source.Source) {
			defer wg.Done()
			if err := src.Run(ctx, ch); err != nil {
				slog.Error("pipeline: source exited with error", "source", src.Name(), "error", err)
			}
		}(src)
	}

	// Close the shared channel once every source has returned, so the
	// processor loop below can drain and exit cleanly.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
		close(done)
	}()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			p.process(ctx, ev)
		case <-ctx.Done():
			// Drain remaining events after sources exit so we don't lose them.
			<-done
			for ev := range ch {
				p.process(context.Background(), ev)
			}
			return nil
		}
	}
}

func (p *Pipeline) process(ctx context.Context, ev *event.Event) {
	matches := p.Rules.Evaluate(ev, p.Facts)
	if len(matches) == 0 {
		return
	}
	p.Dispatcher.Dispatch(ctx, ev, matches)
}
