// Command capture-fixture taps the live zkill source, runs the same
// normalize+enrich pipeline as the bot, and writes the first event(s) whose
// post-enrichment Fields satisfy a predicate to disk. Used to build the
// fixture set under testdata/killmails/ for cmd/rule-check.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/joeyciechanowicz/eve-bot/event"
	"github.com/joeyciechanowicz/eve-bot/internal/source/zkill"
)

func main() {
	var (
		predicate    = flag.String("predicate", "true", "expr-lang boolean over Event.Fields; first matching event(s) are captured")
		out          = flag.String("out", "", "output path (single file when count=1; directory or {{.seq}} template when count>1)")
		count        = flag.Int("count", 1, "number of matches to capture before exiting")
		timeout      = flag.Duration("timeout", 5*time.Minute, "give up after this long")
		baseURL      = flag.String("base-url", "https://r2z2.zkillboard.com", "")
		sequencePath = flag.String("sequence-path", "/ephemeral/sequence.json", "")
		pollInterval = flag.Duration("poll-interval", 100*time.Millisecond, "")
	)
	flag.Parse()

	if *out == "" {
		fatal("--out is required")
	}
	if *count < 1 {
		fatal("--count must be >= 1")
	}

	prog, err := expr.Compile(*predicate, expr.AsBool(), expr.AllowUndefinedVariables())
	if err != nil {
		fatal("compile predicate: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, *timeout)
	defer cancelTimeout()

	src := zkill.New(zkill.Config{
		BaseURL:      *baseURL,
		SequencePath: *sequencePath,
		PollInterval: *pollInterval,
	}, nil, noopCheckpointer{})

	events := make(chan *event.Event, 16)
	srcErr := make(chan error, 1)
	go func() { srcErr <- src.Run(ctx, events) }()

	var seen, matched int
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "capture: stopped after %d events, %d matches: %v\n", seen, matched, ctx.Err())
			os.Exit(1)
		case err := <-srcErr:
			fatal("source exited: %v", err)
		case ev := <-events:
			seen++
			fmt.Fprintf(os.Stderr, "\rcapture: scanned=%d matched=%d", seen, matched)
			ok, err := evalPredicate(prog, ev)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\npredicate eval error on %s: %v\n", ev.ID, err)
				continue
			}
			if !ok {
				continue
			}
			matched++
			path := outputPath(*out, *count, matched, ev)
			if err := writeFixture(path, ev); err != nil {
				fatal("\nwrite %s: %v", path, err)
			}
			fmt.Fprintf(os.Stderr, "\ncapture: wrote %s (%s)\n", path, ev.ID)
			if matched >= *count {
				cancel()
				return
			}
		}
	}
}

func evalPredicate(prog *vm.Program, ev *event.Event) (bool, error) {
	env := make(map[string]any, len(ev.Fields)+4)
	maps.Copy(env, ev.Fields)
	env["event_id"] = ev.ID
	env["event_source"] = ev.Source
	env["event_type"] = ev.Type
	env["occurred_at"] = ev.OccurredAt
	v, err := expr.Run(prog, env)
	if err != nil {
		return false, err
	}
	hit, _ := v.(bool)
	return hit, nil
}

func outputPath(out string, count, matched int, ev *event.Event) string {
	if count == 1 {
		return out
	}
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		return filepath.Join(out, fmt.Sprintf("%s.json", ev.ID))
	}
	return fmt.Sprintf("%s.%d.json", out, matched)
}

func writeFixture(path string, ev *event.Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ev.Fields, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

type noopCheckpointer struct{}

func (noopCheckpointer) GetCheckpoint(string) (string, bool) { return "", false }
func (noopCheckpointer) SetCheckpoint(string, string) error  { return nil }

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(2)
}
