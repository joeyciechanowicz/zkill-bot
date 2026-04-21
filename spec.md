# zkill-bot — Generic Event Pipeline Specification

## 1. Purpose

zkill-bot is a generic, always-on event-processing engine. It does one thing:

1. Run a set of pipelines, each of the shape
   `Source → Enrichers → Rules → Actions`,
   continuously, with operational visibility and safe recovery across
   restarts.

The zKillboard killmail feed is one concrete `Source` implementation; other
sources (ESI, a wormhole-connections API, Discord slash commands) plug in the
same way.

## 2. Core types

All four stages operate on one value type:

```go
type Event struct {
    ID         string         // "<source>:<natural-id>", used for idempotency
    Source     string         // "zkill" | "whapi" | "esi" | "discord" | ...
    Type       string         // source-defined, e.g. "killmail"
    OccurredAt time.Time
    Fields     map[string]any // dotted-path addressable; nested maps/slices
}
```

The interfaces are deliberately small:

```go
type Source   interface { Name() string; Run(ctx, out chan<- *Event) error }
type Enricher interface { Enrich(ctx, *Event) error }
type Handler  interface { Execute(ctx, *Event, args map[string]any) error }
```

## 3. Pipelines

Each pipeline is a dedicated goroutine pair — one for the source, one for the
processing loop — with a buffered channel between them. A pipeline bundles:

- exactly one `Source`,
- an ordered `Enricher` chain,
- a compiled `Set` of rules,
- an `action.Dispatcher` wired to the shared handler registry.

Pipelines are isolated: an event from the `zkill` source never reaches the
`discord` rule set. This keeps rule expressions schema-scoped.

## 4. Rules

Rules are declared in YAML and compiled once at startup. A rule is:

```yaml
- name: <unique>
  enabled: true
  priority: 10              # lower runs first
  continue: false           # in first-match mode, don't stop here
  when: <expr-lang boolean expression>
  actions:
    - type: <handler name>
      for:  <dotted path to a []any>   # optional; action runs per item
      args: { ... }                    # templated per iteration
```

Two evaluation modes:

- **first-match** — the first matching rule wins; `continue: true` lets
  bookkeeping rules (e.g. fact writers) run before later matching rules.
- **multi-match** — every matching rule runs.

`when` expressions see all `Event.Fields` at the top level (`zkb.total_value`,
`victim.character_id`, …) plus these builtins:

| Name                         | Purpose                                         |
|------------------------------|-------------------------------------------------|
| `fact(scope, key)`           | Load a JSON fact, or `nil` if missing/expired.  |
| `fact_exists(scope, key)`    | Bool.                                           |
| `fact_count(scope, prefix)`  | Number of non-expired keys with given prefix.   |
| `now()`                      | `time.Now().UTC()`.                             |
| `event_id`, `event_source`, `event_type`, `occurred_at` | Event metadata. |

Reserved names (above) cannot be used as event field names.

## 5. Actions

Built-in handlers:

| Type      | Purpose                                                     |
|-----------|-------------------------------------------------------------|
| `console` | JSON line to stdout.                                        |
| `webhook` | POSTs JSON to `args.url`. `args.body` overrides the default. |
| `store`   | Writes a fact (`op: set | inc | merge | delete`).            |
| `reply`   | Sends a `ReplyPayload` on `event.Fields["_reply"]`.         |

Action `args` strings are rendered through Go `text/template`. The template
context is `Event.Fields` at the top level plus:

- `.item` — the current for-each item (or nil)
- `.event_id`, `.event_source`, `.event_type`, `.occurred_at`

Every action invocation is idempotency-gated by
`sha256(rule|type|iter-index|args)` keyed against `Event.ID` in the
`actions_history` table; duplicates are skipped across retries and restarts.

Failed actions retry with exponential backoff up to `retry.max_retries`.

## 6. Persistence (shared across pipelines)

One SQLite database, three tables:

```
facts(scope, key, value JSON, updated_at, expires_at)   -- PK (scope,key)
checkpoints(source PK, value, updated_at)
actions_history(event_id, action_fp, executed_at)       -- PK pair
```

`expires_at = 0` means "never". A background janitor deletes expired facts
and ages out action history at `store.action_history_ttl`.

Each source owns its own checkpoint entry. The zkill source persists its
last-processed sequence id; on start it resumes from `last+1` or fetches a
live sequence from `sequence.json` if no checkpoint exists.

## 7. Observability

- `slog` text handler; structured key/value logs.
- `debug: true` in config upgrades to DEBUG level.
- Action counters (success / failure / retry / skipped) live on the
  `Dispatcher` and can be scraped by the host process.

## 8. Configuration

All runtime config lives in a single YAML file:

```yaml
debug: false
store:    { path, janitor_interval, action_history_ttl }
retry:    { max_retries, base_backoff, max_backoff }
pipelines:
  - name: zkill
    source:    { type: zkill, ... }
    facts:     [ { scope, key_path, alias }, ... ]
    rules:     { mode, rules: [ ... ] }
```

Invalid config fails fast at startup with actionable errors.

## 9. Acceptance criteria

A deployment is considered correct when:

- At least one pipeline runs continuously without manual intervention.
- Its source's checkpoint advances monotonically in `checkpoints`.
- Duplicate action execution is prevented across retries and restarts.
- Expired facts disappear within one janitor tick of their `expires_at`.
