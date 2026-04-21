# Writing rules

Rules live under a pipeline's `rules:` block in `config.yaml`. Each rule
binds a boolean expression to a list of actions.

```yaml
rules:
  mode: first-match      # or: multi-match
  rules:
    - name: high-value-kill
      enabled: true
      priority: 10
      when: 'zkb.total_value > 1e9'
      actions:
        - type: console
```

## Fields

Top-level identifiers in a `when:` expression are the keys of `Event.Fields`
for that source. For the zkill source these are:

| Identifier           | Type               | Notes                                |
|----------------------|--------------------|--------------------------------------|
| `killmail_id`        | int64              |                                      |
| `hash`               | string             |                                      |
| `sequence_id`        | int64              |                                      |
| `uploaded_at`        | time.Time          | zkill upload time                    |
| `killmail_time`      | time.Time          | in-game event time                   |
| `solar_system_id`    | int64              |                                      |
| `solar_system_name`  | string             | from SDE enrichment                  |
| `victim`             | object             | see below                            |
| `attackers`          | []object           | see below                            |
| `attacker_count`     | int                |                                      |
| `final_blow`         | object             | one entry from attackers             |
| `items`              | []object           | victim fittings/cargo                |
| `zkb`                | object             | zKillboard metadata                  |
| `has_capital`        | bool               | any participant is capital-class     |

**Victim / attacker / item shapes** mirror the ESI killmail payload plus SDE
additions: `ship_name`, `ship_group`, `ship_group_id`, `ship_category`,
`meta_level`, `meta_group` (for ships); `name` for items.

## Built-in helpers

All rule expressions can call:

| Call                                  | Returns                             |
|---------------------------------------|-------------------------------------|
| `fact(scope, key)`                    | `any` — JSON-decoded or `nil`       |
| `fact_exists(scope, key)`             | `bool`                              |
| `fact_count(scope, prefix)`           | `int`                               |
| `now()`                               | `time.Time` (UTC)                   |

Plus the full expr-lang builtins: `any`, `all`, `filter`, `map`, `len`,
`contains`, `string`, `int`, date/time functions, etc. See
<https://expr-lang.org/docs/language-definition>.

## Modes

- **first-match** — rules are tried in ascending `priority`; the first match
  wins. A matched rule with `continue: true` does **not** stop evaluation —
  use it for bookkeeping rules (e.g. fact writers) that should run before
  later decision rules.
- **multi-match** — every enabled rule that matches fires.

## Examples

### High-value kill

```yaml
- name: high-value
  priority: 10
  when: 'zkb.total_value > 1e9'
  actions:
    - type: console
    - type: webhook
      args: { url: "https://example/hook" }
```

### Capital involvement

```yaml
- name: capital
  priority: 20
  when: 'has_capital && !zkb.npc'
  actions: [{ type: console }]
```

### Record every attacker's kill (fact writer)

```yaml
- name: record-attacker-kills
  priority: 1
  continue: true            # don't stop the pipeline at the writer
  when: 'true'
  actions:
    - type: store
      for: attackers
      args:
        scope: kill_by_char
        key: '{{ .item.character_id }}'
        op: inc
        field: count
        by: 1
        ttl: 720h             # 30d rolling window
```

### Repeat offender (reads the fact above)

```yaml
- name: repeat-offender
  priority: 30
  when: |
    any(attackers, {
      let f = fact("kill_by_char", string(.character_id));
      f != nil && f.count >= 5
    }) && !zkb.npc
  actions: [{ type: console }]
```

### Trade-hub gank

```yaml
- name: jita-hub
  priority: 40
  when: |
    solar_system_name in ["Jita", "Amarr", "Dodixie", "Rens", "Hek"]
    && zkb.total_value > 5e8
    && !zkb.npc
  actions: [{ type: console }]
```

## Validation

All `when:` expressions are compiled at startup; a syntax error fails the
process with the rule name in the message. Undefined field names are
permitted at compile time and silently yield `nil` at eval — watch for typos.
