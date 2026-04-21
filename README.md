# zkill-bot

A generic event-processing engine. Out of the box it ingests the
[zKillboard](https://zkillboard.com) live killmail feed, but every stage of
the pipeline is pluggable:

```
┌────────┐   ┌──────────┐   ┌───────┐   ┌─────────┐
│ Source ├──▶│ Enrichers├──▶│ Rules ├──▶│ Actions │
└────────┘   └──────────┘   └───────┘   └─────────┘
                                            │
                                            ▼
                                   ┌────────────────┐
                                   │  SQLite facts  │
                                   │  (for later    │
                                   │   enrichment)  │
                                   └────────────────┘
```

- **Sources** produce `Event`s. Killmails, wormhole-connection updates, ESI
  polling, and Discord slash-commands are all just sources.
- **Enrichers** mutate `Event.Fields` — SDE lookups, fact lookups from the
  store, anything a rule might want to read.
- **Rules** are YAML-declarative; the `when:` clause is an
  [expr-lang](https://github.com/expr-lang/expr) boolean expression compiled
  at startup.
- **Actions** are the side effects — console, webhook, fact-store writes,
  Discord replies — with idempotency and retry built in.

See [RULES.md](RULES.md) for the rule language and [spec.md](spec.md) for the
full design.

---

## Quick start

```sh
go build -o zkill-bot ./cmd/zkill-bot
./zkill-bot                       # uses ./config.yaml
./zkill-bot -config /my/cfg.yaml
```

`Ctrl+C` stops the bot; checkpoints are persisted to SQLite (`zkill-bot.db`)
so it resumes where it left off.

## Tests

```sh
go test ./...
```

## Updating static game data

Ship names, item names, and solar-system names are compiled into the binary.
Rebuild them when CCP ships new content:

```sh
go run ./cmd/gen-sde         # from ./eve.db
go run ./cmd/gen-systems     # from ESI
go build -o zkill-bot ./cmd/zkill-bot
```

## Adding a new source

1. Create `internal/source/<name>/` with a `Source` implementing
   [`source.Source`](internal/source/source.go) — `Name()` and `Run(ctx, out)`.
2. Normalize payloads into `event.Event` with `Fields` as nested
   `map[string]any` so rules can address nested values with dots
   (`zkb.total_value`).
3. Register it in `cmd/zkill-bot/main.go` under `buildSource`.
4. Wire it up in `config.yaml` as a new `pipelines:` entry.

Stubs for `whapi`, `esi`, and `discord` show the shape.

## Requirements

- Go 1.25+
- No CGO — uses `modernc.org/sqlite`, so cross-compiling a single binary
  "just works".
