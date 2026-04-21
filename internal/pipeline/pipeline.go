// Package pipeline wires one Source to its Enricher chain, compiled Rule set,
// and action Dispatcher. A Runner manages a slice of Pipelines and runs them
// concurrently until ctx is cancelled.
package pipeline

import (
	"context"
	"log/slog"
	"sync"

	"zkill-bot/internal/action"
	"zkill-bot/internal/enrich"
	"zkill-bot/internal/event"
	"zkill-bot/internal/rules"
	"zkill-bot/internal/source"
)

// Pipeline is a source-specific event-processing pipeline.
type Pipeline struct {
	Name       string
	Source     source.Source
	Enrichers  enrich.Chain
	Rules      *rules.Set
	Dispatcher *action.Dispatcher
	Facts      rules.FactStore // passed to rule expressions
	BufferSize int             // channel buffer between source and processor
}

// Run starts the source goroutine and processes events until ctx is done.
func (p *Pipeline) Run(ctx context.Context) error {
	buf := p.BufferSize
	if buf <= 0 {
		buf = 32
	}
	ch := make(chan *event.Event, buf)

	srcDone := make(chan error, 1)
	go func() {
		srcDone <- p.Source.Run(ctx, ch)
		close(ch)
	}()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return <-srcDone
			}
			p.process(ctx, ev)
		case <-ctx.Done():
			<-srcDone
			// Drain remaining events already in the channel so we don't lose them.
			for ev := range ch {
				p.process(context.Background(), ev)
			}
			return nil
		}
	}
}

func (p *Pipeline) process(ctx context.Context, ev *event.Event) {
	if err := p.Enrichers.Enrich(ctx, ev); err != nil {
		slog.Warn("pipeline: enrich error", "pipeline", p.Name, "event", ev.ID, "error", err)
	}
	matches := p.Rules.Evaluate(ev, p.Facts)
	if len(matches) == 0 {
		return
	}
	p.Dispatcher.Dispatch(ctx, ev, matches)
}

// Runner runs a set of pipelines concurrently.
type Runner struct {
	Pipelines []*Pipeline
}

// Run starts every pipeline and blocks until all exit (ctx cancelled + source
// Run returns).
func (r *Runner) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, p := range r.Pipelines {
		wg.Add(1)
		go func(p *Pipeline) {
			defer wg.Done()
			if err := p.Run(ctx); err != nil {
				slog.Error("pipeline: exited with error", "pipeline", p.Name, "error", err)
			}
		}(p)
	}
	wg.Wait()
	return nil
}
