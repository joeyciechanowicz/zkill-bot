# Writing Rules

Rules tell zkill-bot which killmails to care about and what to do when one matches. They live in `config.yaml` under the `rules:` section.

---

## The basics

Every rule has four parts:

```yaml
- name: "my-rule" # A label for this rule (used in logs)
  enabled: true # true = active, false = ignored
  priority: 10 # Lower number = checked first
  filter: # The condition that must be true
    solo: true
  actions: # What to do when it matches
    - type: console
```

Rules are checked in priority order. The rule with the lowest priority number is checked first.

---

## Modes

At the top of the rules section you set `mode`:

```yaml
rules:
  mode: first-match # Stop after the first rule that matches (default)
  # mode: multi-match # Run every rule that matches
  rules:
    - ...
```

**first-match** is usually what you want — one kill triggers one notification. Use **multi-match** if you want a single kill to potentially trigger several different rules (e.g. a corp-loss rule _and_ a high-value rule at the same time).

---

## Actions

Actions are what happens when a rule matches.

### Console

Prints a one-line summary to the terminal. Useful for testing.

```yaml
actions:
  - type: console
```

### Webhook (Discord)

Posts a notification to a Discord channel via a webhook URL. To get a webhook URL, go to your Discord channel settings → Integrations → Webhooks → New Webhook → Copy URL.

```yaml
actions:
  - type: webhook
    args:
      url: "https://discord.com/api/webhooks/..."
      template: "default" # or "capital" or "loss"
```

**Templates:**

| Template  | Best for                                                |
| --------- | ------------------------------------------------------- |
| `default` | General kills — red embed                               |
| `capital` | Capital ship kills — purple embed, lists attacker ships |
| `loss`    | Losses for your corp/alliance — orange embed            |

You can add both `console` and `webhook` to the same rule:

```yaml
actions:
  - type: console
  - type: webhook
    args:
      url: "https://discord.com/api/webhooks/..."
      template: "default"
```

---

## Filters

The `filter:` block describes what a killmail must look like to match the rule. If you write multiple conditions directly under `filter:`, **all of them must be true** at once.

---

### Kill type

| Filter              | Values           | What it means                                                                            |
| ------------------- | ---------------- | ---------------------------------------------------------------------------------------- |
| `solo: true`        | `true` / `false` | zKillboard flagged this as a solo kill                                                   |
| `npc: true`         | `true` / `false` | The kill involved NPCs                                                                   |
| `awox: true`        | `true` / `false` | The victim was killed by their own corp                                                  |
| `has_capital: true` | `true` / `false` | A capital ship (Titan, Dreadnought, Carrier, Supercarrier, Force Auxiliary) was involved |

```yaml
filter:
  solo: true
  npc: false
```

---

### ISK value

Values are in ISK. No commas — use full numbers or scientific-style zeros.

| Filter                      | What it means                                           |
| --------------------------- | ------------------------------------------------------- |
| `zkb_value_min: 1000000000` | Total value must be at least this much (1 billion here) |
| `zkb_value_max: 5000000000` | Total value must be no more than this much              |

```yaml
filter:
  zkb_value_min: 500000000 # 500 million ISK minimum
```

**ISK reference:**

| Amount      | How to write it |
| ----------- | --------------- |
| 100 million | `100000000`     |
| 500 million | `500000000`     |
| 1 billion   | `1000000000`    |
| 10 billion  | `10000000000`   |

---

### Number of attackers

| Filter                   | What it means                    |
| ------------------------ | -------------------------------- |
| `attacker_count_min: 5`  | At least this many attackers     |
| `attacker_count_max: 10` | No more than this many attackers |

```yaml
filter:
  attacker_count_min: 10 # blob fight
```

---

### Location

**By system name** (easiest):

```yaml
filter:
  solar_system_name: ["Jita", "Amarr", "Dodixie"]
```

Names are not case-sensitive. List as many systems as you like.

**By system ID** (for systems without memorable names):

```yaml
filter:
  solar_system_id: [30000142, 30002187]
```

You can find system IDs on [zkillboard.com](https://zkillboard.com) or [dotlan.net](http://evemaps.dotlan.net).

**By zKillboard location label:**

zKillboard tags every kill with labels like `loc:highsec`, `loc:lowsec`, `loc:nullsec`, `loc:wspace` (wormhole).

```yaml
filter:
  zkb_label: ["loc:lowsec"]
```

You can also list multiple labels — the kill only needs to match one:

```yaml
filter:
  zkb_label: ["loc:nullsec", "loc:wspace"]
```

---

### Time of day / day of week

All times are **UTC**.

**Time window:**

```yaml
filter:
  time_window:
    from: "18:00"
    to: "23:00"
```

Windows that cross midnight work too:

```yaml
filter:
  time_window:
    from: "22:00"
    to: "04:00" # runs 22:00 → 04:00 UTC
```

**Day of week:**

```yaml
filter:
  day_of_week: ["saturday", "sunday"]
```

Valid values: `monday` `tuesday` `wednesday` `thursday` `friday` `saturday` `sunday`

---

### Victim filters

These match against the person or ship that was destroyed.

| Filter                              | What it means                              |
| ----------------------------------- | ------------------------------------------ |
| `victim_character_id: [12345]`      | The victim is this character               |
| `victim_corporation_id: [98812444]` | The victim is in this corporation          |
| `victim_alliance_id: [99001234]`    | The victim is in this alliance             |
| `victim_ship_type_id: [670]`        | The victim was flying this exact ship type |
| `victim_ship_group_id: [25]`        | The victim was flying a ship in this group |

You can put multiple IDs in the list — the kill matches if it's any one of them.

To find corporation or alliance IDs, go to the entity's page on zkillboard.com — the number in the URL is the ID. For example `https://zkillboard.com/corporation/98812444/` → ID is `98812444`.

---

### Attacker filters

These match against the people doing the killing. A kill matches if **any one attacker** meets the condition.

| Filter                                | What it means                               |
| ------------------------------------- | ------------------------------------------- |
| `attacker_character_id: [12345]`      | One of the attackers is this character      |
| `attacker_corporation_id: [98812444]` | One of the attackers is in this corporation |
| `attacker_alliance_id: [99001234]`    | One of the attackers is in this alliance    |
| `attacker_ship_type_id: [11567]`      | One of the attackers flew this ship type    |

---

### Item filters

Matches if the victim's fitting or cargo contained a specific item.

```yaml
filter:
  item_type_id: [2836, 2834] # any of these items must be present
```

Item type IDs can be found on [everef.net](https://everef.net) or by searching on [zkillboard.com](https://zkillboard.com).

---

## Combining conditions with and / or / not

By default, multiple filters on the same rule all need to be true. To build more complex logic, use `and`, `or`, and `not`.

### and — all must be true

```yaml
filter:
  and:
    - solo: true
    - zkb_value_min: 100000000
    - npc: false
```

This is the same as writing the filters directly under `filter:` — every condition must match.

### or — at least one must be true

```yaml
filter:
  or:
    - victim_corporation_id: [98812444]
    - victim_alliance_id: [99001234]
```

Matches if the victim is in that corp **or** that alliance.

### not — must not be true

```yaml
filter:
  not:
    npc: true
```

Matches kills that are **not** NPC kills.

### Nesting them together

You can nest these as deeply as you need:

```yaml
filter:
  and:
    - zkb_value_min: 500000000
    - or:
        - solar_system_name: ["Jita", "Amarr"]
        - has_capital: true
    - not:
        npc: true
```

Read this as: _"Value at least 500M ISK, AND (in Jita or Amarr OR involves a capital), AND not an NPC kill."_

---

## Full examples

### Alert when your corp loses a ship

```yaml
- name: "corp-loss"
  enabled: true
  priority: 10
  filter:
    victim_corporation_id: [98812444]
  actions:
    - type: console
    - type: webhook
      args:
        url: "https://discord.com/api/webhooks/..."
        template: "loss"
```

### Capital kill anywhere

```yaml
- name: "capital-kill"
  enabled: true
  priority: 20
  filter:
    has_capital: true
  actions:
    - type: webhook
      args:
        url: "https://discord.com/api/webhooks/..."
        template: "capital"
```

### Big kill in a trade hub

```yaml
- name: "trade-hub-big-kill"
  enabled: true
  priority: 30
  filter:
    and:
      - solar_system_name: ["Jita", "Amarr", "Dodixie", "Rens", "Hek"]
      - zkb_value_min: 1000000000
      - npc: false
  actions:
    - type: webhook
      args:
        url: "https://discord.com/api/webhooks/..."
        template: "default"
```

### EU prime-time solo kills, low or null sec only

```yaml
- name: "eu-prime-solo"
  enabled: true
  priority: 40
  filter:
    and:
      - solo: true
      - npc: false
      - time_window:
          from: "17:00"
          to: "22:00"
      - or:
          - zkb_label: ["loc:lowsec"]
          - zkb_label: ["loc:nullsec"]
  actions:
    - type: console
```

### Any kill in wormhole space

```yaml
- name: "wormhole-kill"
  enabled: true
  priority: 50
  filter:
    and:
      - zkb_label: ["loc:wspace"]
      - npc: false
  actions:
    - type: webhook
      args:
        url: "https://discord.com/api/webhooks/..."
        template: "default"
```

To target a specific wormhole system, use its J-name instead:

```yaml
filter:
  solar_system_name: ["J132737"]
```

### Watch a specific pilot

```yaml
- name: "watch-pilot"
  enabled: true
  priority: 5
  filter:
    or:
      - victim_character_id: [2115668403]
      - attacker_character_id: [2115668403]
  actions:
    - type: webhook
      args:
        url: "https://discord.com/api/webhooks/..."
        template: "default"
```

---

## Tips

- **Indentation matters.** YAML uses spaces (not tabs) to show structure. Keep things lined up consistently or the file won't load.
- **Test with `console` first.** Add `- type: console` to a rule while you're building it so you can see in the terminal whether it's matching what you expect before enabling Discord notifications.
- **Disable without deleting.** Set `enabled: false` to turn a rule off without removing it.
- **Priority gaps are fine.** Using 10, 20, 30 instead of 1, 2, 3 leaves room to insert rules between existing ones later without renumbering.
- **ISK values have no commas or spaces.** Write `1000000000` not `1,000,000,000`.
- **System names are not case-sensitive.** `"jita"` and `"Jita"` both work.
