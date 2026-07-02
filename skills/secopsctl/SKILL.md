---
name: secopsctl
description: >
  Operating guide for AI agents driving the secopsctl CLI against a Google
  SecOps instance (Chronicle SIEM + Siemplify SOAR). Encodes the two-auth-plane
  model, the full command map, the mutation ritual, the config-as-code loop,
  self-discovery commands, the agent-first search/gemini surfaces and their JSON
  output contracts, end-to-end recipes, enum values, and the gotchas the
  per-command --help can't express. Self-served: `secopsctl skill` prints it from
  the binary. Read this before issuing any secopsctl command.
---

# secopsctl agent operating guide

secopsctl operates a Google SecOps instance — **Chronicle SIEM** and **Siemplify
SOAR** — as code. This guide makes you productive without the repo docs; the live
commands (`commands --json`, `status surfaces`, `<cmd> --help`) are the source of
truth when something here looks out of date.

## Session bootstrap — do these first

```bash
secopsctl doctor                      # config + auth + SIEM/SOAR reachability (read-only)
secopsctl status capabilities --json  # version, per-surface status, auth health per plane, read-only state
secopsctl commands --json             # every verb: path, kind (read/guarded-mutation), flags, --json support
```

`doctor` is the gate: if either plane reports unhealthy, fix auth before proceeding.
`status capabilities --json` and `commands --json` are the **live source of truth**
for what this binary supports — prefer them over any static list; surfaces, names,
and flags evolve.

## The two auth planes

Every command runs against one of two **independent** credential planes — a SOAR
AppKey call works even when SIEM ADC is expired, and vice-versa.

| Plane | Host | Auth | Commands |
|---|---|---|---|
| **SIEM** (Chronicle) | `{region}-chronicle.googleapis.com` | Google ADC / OAuth (minted in-process, never on disk) | `pull`, `push`, `drift`, `search`, `gemini`, `rules` (+ `curated`, `exclusions`), `ti`, `lists`, `dashboards`, `entities`, `alerts`, `cases`, `ingest`, `data-access`, `status` |
| **SOAR** (Siemplify) | `{tenant}.siemplify-soar.com` | AppKey (`soar_app_key` in config or `$SECOPS_SOAR_APP_KEY`; no ADC) | `soar pull/push`, `playbooks`, `integrations`, `soar jobs/ide/settings/connector/audit/legacy/users`, `cases` (SOAR-host triage), `content-hub` |

### SIEM auth recovery

The ADC token carries a short-lived RAPT. When a SIEM call fails with `invalid_grant`
or `"reauth … invalid_rapt"`:

1. Re-auth: `gcloud auth login` (or `gcloud auth application-default login`).
2. Mint a fresh token **in the same shell**: `SECOPS_ACCESS_TOKEN=$(gcloud auth print-access-token) secopsctl ...`
3. Confirm: `secopsctl doctor`.

SOAR (AppKey) is **unaffected** by an ADC lapse — keep doing SOAR work meanwhile.

### Config resolution (highest priority first)

`SECOPS_*` env vars → `--config <path>` / `$SECOPSCTL_CONFIG` → `~/.secopsctl/instance.yaml`
→ `./config/instance.yaml` → `~/.config/secopsctl/instance.yaml`. Inspect the active
config with `secopsctl info` (AppKey redacted).

## Command map (the full surface, by group)

One row per top-level group; run `<group> --help` or `commands --json` for the exact
verbs and flags. Kind: **read** is always safe; **guarded** needs the dry-run → `--yes`
ritual.

| Group | What | Plane | Kind |
|---|---|---|---|
| `search` | deterministic SIEM search: `udm` · `raw` · `stats` · `event <id>` · `export` · `validate` · `run` · `saved` | SIEM | read (`saved save/share/delete` guarded) |
| `gemini` | AI hub: `generate-query` (NL→UDM) · `search` (NL→UDM + run) · `ask` (assistant) · `investigate` · `summarize` · `generate` (playbook) | SIEM+SOAR | read |
| `rules` | **your CUSTOM detections** — inspect (`list` · `get` (current state + YARA-L) · `detections` · `test` (streams) · `validate` · `trends` · `errors` · `alerts` · `versions`+`diff` · `health`) + lifecycle (`promote`, `duplicate`, `versions restore`, `retrohunt`) | SIEM | read + guarded (`promote`, `duplicate`, `versions restore`, `retrohunt create`) |
| `curated` | **Google-managed PREDEFINED detections** — `categories` · `rule-sets` (default enabled, `--all`/`--search`/`--category`) · `search` (unified across sets+rules, `--installed`/`--tactic`/`--severity`) · `rules --set <id>` · `rule <id>` · `detections` · `events` · `trends` · `set` (toggle enable/alerting) | SIEM | read + guarded (`set`) |
| `exclusions` | findings refinements (apply to **custom + curated**): `list` · `get` · `deploy` | SIEM | read + guarded (`deploy`) |
| `mitre` | ATT&CK coverage across custom + curated (`--type custom\|curated\|all`) | SIEM | read |
| `ti` | threat intel & IOCs: `find` · `get` · `related` · `collections` · `collection` · `collection-matches` | SIEM | read |
| `lists` | `empty` (reference list) · `watchlists …` | SIEM | read + guarded |
| `dashboards` | `create` · `list` · `get` · `edit` · `charts` (list/get/add/batch/edit/remove/run) · `markdown` (add/edit/remove) · `button` (add/edit/remove) · `layout` (show/move) · `filters` (show/set) · verify · lint/fix/inspect · export/import · duplicate · delete | SIEM | read + guarded |
| `entities` | `summarize` · `graph` · `risk-scores` | SIEM | read |
| `alerts` | `list` · `get` · `update` (feedback) | SIEM | read + guarded (`update`) |
| `cases` | SOAR case triage: list/get/close/assign/tag/stage/comment/run-action/incident/report/alert… | SOAR | read + guarded |
| `content-hub` | `browse` · `list` · `get` · `contentpacks` · `featured` · `diff` · `install` · `uninstall` | SOAR | read + guarded (`install`/`uninstall`) |
| `ingest` | `feeds` · `forwarders` · `parsers` · `log-types` · `pipeline` · `health` | SIEM | read + guarded |
| `data-access` | RBAC: `labels …` · `scopes …` | SIEM | read + guarded |
| `status` | `capabilities` · `coverage` · `surfaces` (read-only diagnostics) · `enums [--live] [--json]` (SOAR integer-to-name enum mappings; `--live` adds instance-specific values) | both/offline | read |
| `playbooks` | `list` · `get` · `lint` · `health` · `diff` · `duplicate` · `deploy` · `delete` · `move` · `categories` · `validate` · `run` · `debug` · `export` · `import` · `generate` · … | SOAR | read + guarded |
| `integrations` | `list [--instances]` · `get` · `test` · `create` · `delete` · `configure` · `rename` · `install` · `uninstall` · `instances` · `connector` · `scaffold` · `action` · `job` | SOAR | read + guarded |
| `soar` | `pull` · `push` · `jobs` · `ide` · `settings` · `connector` · `audit` · `legacy` · `users` | SOAR | read + guarded |
| `pull` / `drift` | snapshot live state / report drift (the as-code loop) | SIEM | read |
| `push` | deploy config-as-code (rules-create/update/deploy/disable, reconcile surfaces) | SIEM | guarded |

> **Names renamed (no aliases).** `query`→`search`; NL/Gemini→`gemini`;
> `rule-exclusions`→`exclusions`; `indicators`/`threat-intel`→`ti`;
> `reference-lists`/`watchlists`→`lists`; `soar marketplace`→`content-hub`;
> `feeds`/`parsers`/… →`ingest …`; `capabilities`/`coverage`/`surfaces`→`status …`;
> `soar case`→`cases`. **Top-level rule split (v0.6.1):** `rules` = your custom
> detections; `curated` = Google-managed predefined detections; `exclusions` and
> `mitre` are top-level (they span both). **pull/push TARGET args are unchanged**
> (they mirror the on-disk dirs): `pull reference_lists`, `push curated`, `pull feeds`,
> etc. stay snake_case.

## The mutation ritual — every guarded verb

`pull`/`drift`/`list`/`get`/`search` are read-only. Every verb that changes live state
is **guarded**: it defaults to a dry-run preview with a `LIVE DEPLOY` banner; pass
`--yes` to apply.

```bash
secopsctl push rules-update            # 1. dry-run preview (default)
secopsctl push rules-update --yes      # 2. apply, after reading the preview
```

**Never skip the preview.** A `push` is a production deploy to a live instance. After
a successful mutation, **re-pull** the surface so local files match live (done ≠
committed; done = deployed AND mirrored).

### Hard read-only mode for automation

Launch autonomous agents with `SECOPS_READONLY=1` (or `--read-only`): every guarded
mutation degrades to a dry-run even with `--yes`, and AI generations that create
server-side artifacts are refused. Every confirmed mutation or refusal appends one
JSONL record to `~/.secopsctl/audit.jsonl`.

### --prune — delete server-only objects

`push <target> --prune` deletes live objects with no local file. Off by default;
requires a fresh pull this session. Not every surface is prune-eligible — check with
`secopsctl status surfaces` (PRUNE column) or `push <target> --help`.

## The config-as-code loop

```text
pull live state  →  review in git diff  →  push back  →  re-pull
```

```bash
secopsctl pull rules                   # snapshot to local files
git diff                               # the review surface
secopsctl push rules-update --dry-run  # preview
secopsctl push rules-update --yes      # deploy
secopsctl pull rules                   # re-pull so local matches live
```

**Always pull before editing** — live UI edits happen out-of-band; stale local state
silently clobbers them on push. The same loop applies to every reconcile surface:
`reference_lists`, `data_tables`, `feeds`, `parsers`, `dashboards`, `forwarders`,
`rule_exclusions`, `soar/webhooks`, `soar/playbooks`, `soar/connectors`, `soar/jobs`.
**These pull/push target names are snake_case and unchanged** — only the imperative
command *groups* (`ingest`, `curated`, …) were renamed. `secopsctl status surfaces`
lists each surface's lane and `--prune` eligibility.

## Command self-discovery

Do not guess command names, flags, or enums — read the live catalog:

- `secopsctl commands --json` — every verb: `path`, `kind`, per-flag `{type, default, required, enum, usage}`, `json` support, an example.
- `secopsctl status capabilities --json` — version, auth health per plane, per-surface status (validated vs blocked), read-only state.
- `secopsctl status surfaces [--json]` — every API surface family: plane, version, lane, status, `--prune` eligibility.
- `<cmd> --help` — per-command flags plus plane/version/write gotchas.

Build an allowlist by filtering `commands --json`: `kind == "read"` is always safe;
`kind == "guarded-mutation"` needs the dry-run → `--yes` ritual.

## Searching (deterministic) — `search`

`search` is the SIEM hunt surface. All reads; output is agent-tunable.

```bash
secopsctl search udm 'metadata.event_type = "NETWORK_CONNECTION"' --hours 6
secopsctl search udm 'principal.hostname = "host-01"' --from 2024-01-02T00:00:00Z --to 2024-01-03T00:00:00Z
```

- **`udm <filter>`** — point-in-time event search over a window (`--hours`, or `--from`/`--to`).
  `--limit` caps results (the simple path silently truncates past it — a stderr warning fires).
  **`--all`** switches to the complete-results engine: it returns the full set up to the
  limit AND reports the **total match count** (so you know how much you didn't get).
- **`raw <pattern>`** — content regex over the raw ingested bytes (reaches logs with no parser).
- **`stats <agg>`** — aggregation query (`match:`/`outcome:`/`order:`); see the recipe below.
- **`event <id>`** — drill into ONE event by id: enriched UDM (default), `--udm` (unenriched), `--raw` (original log line).
- **`export <filter>`** — server-side CSV of **all** matches (not capped at `--limit`) over the window (`--hours` default 24, or `--from`/`--to`); `--fields` (column labels), `--out <file>`.
- **`validate <query>`** — syntax-check a UDM query without running it; exits non-zero on an invalid query (gate on it).
- **`run --file <f>`** — run a UDM predicate from a tracked `.udm` file (or stdin).
- **`saved`** — server-side saved & shared searches (the console's Search Manager):
  `saved list/get/run <id>`, and the guarded `saved save --name … (--query|--file) [--share]`,
  `saved share/unshare <id>`, `saved delete <id>`. `--share` makes a search org-wide. The
  guarded `saved` verbs follow the dry-run → `--yes` ritual (preview by default), like every mutation.

### Agent-first output (every `search` read, and `gemini search`)

- `--format jsonl|json|csv|table` — default **table** on a terminal, **jsonl** when piped
  (one event per line → stream/grep/`jq` per record). `json` is the full indented array.
- `--fields a,b,c` — project dotted UDM paths (e.g. `metadata.event_type,principal.hostname`);
  tolerant of camelCase/snake_case and the `{udm}` vs `{event}` result shapes.
- `--out <file>` — write results to a file instead of stdout.
- The global `--json` still works (= `--format json`).

## AI search — `gemini`

Gemini-powered (the console's "Get the help of AI" / "Gemini Investigations"). One-time
account opt-in is needed (`gemini ask --opt-in`); read-only.

```bash
secopsctl gemini generate-query 'failed admin logins in the last hour'  # NL → UDM query (don't run)
secopsctl gemini search   'network connections to a public IP in the last hour'  # NL → UDM + run
secopsctl gemini ask      'how do I write a YARA-L rule for process injection?'  # assistant Q&A
secopsctl gemini investigate <alert-id>                                  # shows existing result or triggers new
secopsctl gemini investigate <alert-id> --rerun                          # force new investigation
secopsctl gemini investigate <alert-id> --latest                         # read-only: show existing only
secopsctl gemini summarize --case-id <id>                               # AI case summary (moved from cases)
```

`gemini generate-query`/`search` honor the **time window the model infers** from the text
("…in the last hour") unless you set `--hours`/`--from` explicitly. `gemini search`/`search generate-query`
takes the same `--format`/`--fields`/`--out` flags as `search udm`.

## Output & JSON contracts

Pass `--json` (or `--format json`) on any read command for parseable output (the human
table is for people). The global `--output table|json|csv` is equivalent where
supported: `--output json` ≡ `--json` on every command; `--output csv|table` applies
on the format-aware commands (`query udm`, `mitre`, `rules health`), where a local
`--format` overrides it. Under `--json`, a failure prints a structured `{code, message,
retryable, status, request_id}` envelope on **stderr** while stdout stays clean for the
payload — so branch on exit code, parse stdout, surface stderr. For bulk event work
prefer `--format jsonl` (per-line) or `search export` (server-side CSV, uncapped).

## Common recipes

End-to-end, copy-pasteable. Replace placeholders; preview before `--yes`.

### Hunt UDM events

```bash
secopsctl search udm 'metadata.event_type = "NETWORK_DNS"' --hours 6 --format jsonl
secopsctl search udm 'metadata.event_type = "NETWORK_DNS"' --all --fields metadata.event_timestamp,principal.hostname
secopsctl search export 'metadata.event_type = "NETWORK_DNS"' --hours 24 --fields timestamp,user,hostname --out dns.csv
secopsctl search event 'AAAA…=' --raw      # the original raw log behind one event
```

Pull raw logs for a broken/missing parser (events normalize to `GENERIC_EVENT`), pipe
straight into a parser test:

```bash
secopsctl search udm 'metadata.log_type = "KONG_GATEWAY" AND metadata.event_type = "GENERIC_EVENT"' \
    --raw --limit 50 | secopsctl ingest parsers run KONG_GATEWAY --cbn parser.conf --logs -
# statedump diagnostics are auto-injected; use --statedump for verbose output on every log
secopsctl ingest parsers run KONG_GATEWAY --cbn parser.conf --logs sample.txt --statedump
```

### Parser management — upgrade, rollback, extractors

```bash
secopsctl ingest parsers upgrade FORTINET_FIREWALL              # preview release candidate (dry-run)
secopsctl ingest parsers upgrade FORTINET_FIREWALL --yes        # activate it
secopsctl ingest parsers rollback FORTINET_FIREWALL             # preview rollback candidate
secopsctl ingest parsers rollback FORTINET_FIREWALL --yes       # apply rollback

secopsctl ingest parsers extension extract GCP_DNS              # discover extractable raw log fields
secopsctl ingest parsers extension extract GCP_DNS --all --yes  # create extractor extension for all fields
secopsctl ingest parsers extension extract GCP_DNS --fields insertId,logName --yes  # specific fields
secopsctl ingest parsers extension setting GCP_DNS              # show current extraction type
secopsctl ingest parsers extension setting GCP_DNS all --yes    # extract all fields (up to 100)

secopsctl ingest log-types list                                 # active log types (feedCount > 0)
secopsctl ingest log-types list --all --sort feeds              # full catalog, sorted by feed count
secopsctl ingest log-types create MY_LOG "My Log" --yes         # create custom log type (adds _CUSTOM suffix)
```

### Aggregate (stats) — the YARA-L a dashboard chart uses

`search udm` rejects an aggregation; `search stats` runs it. `match:` declares the
group-by, `outcome:` the measures, `order:` the sort:

```bash
secopsctl search stats --hours 24 'metadata.log_type != ""
match: metadata.log_type
outcome: $c = count(metadata.id)
order: $c desc'
```

A free-standing `search stats` takes a **bare field** in `match:` (`metadata.log_type`),
not an assignment; the `outcome:` declares the measures. Validate a chart query with
`search stats` **before** `dashboards charts add`. Don't name a `match:`/`outcome:`
variable with a reserved YARA-L keyword (e.g. `$rule`, `$events`) — it compiles but
fails at execute time; `charts add`/`charts edit` warn when you do. The full reserved
list is in `charts add --help`. `dashboards verify` rewrites the opaque "no viable
alternative" 400 into an actionable "reserved-word variable $X — rename it" message.

### Build and manage dashboards

**The 96-column grid.** A dashboard is a grid **96 units wide**; height grows
unbounded. Every widget (chart, markdown, button) has a position:
`startX`/`startY` (top-left corner) and `spanX`/`spanY` (width/height).

Recommended layout sizes:

| Widget purpose | spanX (width) | spanY (height) | Notes |
|---|---|---|---|
| Full-width chart | 96 | 16 | Default — one chart per row |
| Half-width chart | 48 | 16 | Two charts side by side |
| Third-width chart | 32 | 16 | Three across |
| Quarter-width chart | 24 | 12–16 | Four across (compact) |
| Markdown heading/notes | 96 | 4–8 | Full-width text block |
| Sidebar markdown | 24–32 | 16 | Text next to a chart |

```bash
secopsctl dashboards create --name "SOC Overview" --access public --yes # create empty
secopsctl dashboards get <id>                                          # metadata summary
secopsctl dashboards edit <id> --name "New Name" --access public --yes # edit metadata
secopsctl dashboards layout show <id>                                  # grid map of all widgets
secopsctl dashboards layout move <id> --widget-id <w> --x 0 --span-x 48 --yes  # resize/reposition
secopsctl dashboards filters show <id>                                 # current global time filter
secopsctl dashboards filters set <id> --time 7 --unit DAY --yes        # set time range

# Chart authoring (validate the query first with `search stats`)
secopsctl dashboards charts add <id> --title "DNS by host" \
    --query 'metadata.event_type = "NETWORK_DNS" match: principal.hostname outcome: $c = count(metadata.id)' \
    --chart-type bar --x principal.hostname --y '$c' \
    --layout '{"startX":0,"spanX":48,"startY":0,"spanY":16}' --yes
secopsctl dashboards charts list <id>                                  # list charts + queries + filtersIds
secopsctl dashboards charts get <chart-id>                             # full detail: viz, query, filters, layout
secopsctl dashboards charts run <id> --chart-id <cid> --table          # execute query → readable table output
secopsctl dashboards charts edit <id> --chart-id <cid> --title "New name" --yes  # rename in place
secopsctl dashboards charts edit <id> --chart-id <cid> --filters GlobalTimeFilter --yes  # bind to time filter

# Stacked bar/line (--series-by puts seriesColumn at viz top level)
secopsctl dashboards charts add <id> --title "By actor" \
    --query '...' --chart-type bar --x actor --y count --series-by dataset --yes

# Markdown widgets (static text — no query)
secopsctl dashboards markdown add <id> --title "Notes" \
    --text "## Security Overview\nKey metrics for the SOC." \
    --background-color "#f5f5f5" --yes

# Button widgets (hyperlink tiles)
secopsctl dashboards button add <id> --title "Docs" \
    --label "Open docs" --url "https://example.com" \
    --style filled --color "#4285f4" --yes
```

Three tile types: `visualization` (charts with queries), `button` (hyperlinks),
`markdown` (static text). Charts need a query + interval; markdown needs only
`--text`; buttons need `--label` + `--url`. All share the same grid layout.

### SOC triage — queue → case → AI verdict → close

```bash
secopsctl cases list --status open --sort priority --json   # the queue, worst first
secopsctl cases aging --limit 20                            # oldest open cases + SLA status
secopsctl cases workload                                    # open-case load per analyst
secopsctl cases get <id> --json                             # case + alerts (+ firing rule per alert)
secopsctl cases overview --id <id>                          # entities + enrichment behind the Overview tab
secopsctl cases summarize --id <id>                         # Google AI summary: summary/reasons/next steps
secopsctl cases run-action --id <id> --action <name> --instance <uuid> --dry-run  # run an integration action
secopsctl cases close --id <id> --reason not-malicious \
    --root-cause '<your-root-cause>' --comment 'false positive' --yes
```

Bulk: `cases assign|tag|stage --ids 1,2,3` acts on a set in one call; `cases stats`
gives open/closed counts + age/resolution percentiles. Per-**alert** triage (close one
alert without closing the case): add `--alert <alert-id>` to `close`, or `cases alert
<verb>`. Sort the alert queue too: `secopsctl alerts list --sort priority`.

### Ship and tune a detection rule

```bash
secopsctl rules get <rule>                                         # current state (running? alerting?) + YARA-L; --text for raw, --json for full
secopsctl rules test detections/new-rule.yaral --hours 24          # PREVIEW detections vs history — STREAMS progress + hits (no deploy)
secopsctl rules test detections/new-rule.yaral --from <ts> --to <ts> --json   # explicit window; --no-stream to buffer
secopsctl rules promote detections/new-rule.yaral --dry-run        # validate + create + deploy in one step
secopsctl rules promote detections/new-rule.yaral --alerting=false --yes
secopsctl rules duplicate <rule> --name <new> --dry-run            # clone YARA-L under a new name (created disabled)
secopsctl pull rules && git diff && secopsctl push rules-deploy --dry-run   # tune tracked rules
secopsctl rules trends --hours 168                                 # noisiest rules, to drive tuning
secopsctl rules health --only silent                               # which rules compile but never fire / error
secopsctl mitre --type all --format json                     # MITRE ATT&CK coverage (technique × rule count)
secopsctl rules versions <rule>                                    # revision history; `diff <rule> 1 2`; guarded `restore <rule> <v>`
secopsctl curated categories                                       # 12-row overview: category names + set/enabled counts
secopsctl curated rule-sets                                        # enabled (installed) sets with deployment state
secopsctl curated rule-sets --all --search azure                   # search the full catalog
secopsctl curated rule-sets --category "Windows Threats"            # filter by category name or UUID
secopsctl curated search powershell                                # unified search across rule sets AND rules
secopsctl curated search --tactic TA0003 --installed               # MITRE filter, installed only
secopsctl curated rules --set <id>                                 # individual rules in one set
secopsctl curated rule <ur_id>                                     # rule detail with resolved set/category names
secopsctl curated set --category <cat> --ruleset <rs> --precision broad --alerting=false --dry-run  # toggle
```

Investigate: `secopsctl entities graph <detection-id>` walks the findings-graph pivot;
`secopsctl entities risk-scores --order-by 'riskScore desc'` ranks hosts/users.

### Install Content Hub content

```bash
secopsctl content-hub browse                                       # integration + content-pack totals
secopsctl content-hub list                                         # the catalog + identifiers (--installed for installed only)
secopsctl content-hub install --identifier <id> --dry-run          # → --yes to apply
```

### Reconcile any surface (generic)

```bash
secopsctl soar pull connectors
git diff
secopsctl soar push connectors --prune --dry-run                   # --prune deletes live objects with no local file
secopsctl soar push connectors --yes
secopsctl soar pull connectors
```

## Enums & values you'll need

The CLI takes **names**, not the server's magic ints. Common sets:

| Where | Valid values |
|---|---|
| `cases close --reason` / `soar push bulk-close` | `malicious` · `not-malicious` · `maintenance` · `inconclusive` · `unknown` (a false positive → `not-malicious`) |
| `cases list --priority` | `informative` · `low` · `medium` · `high` · `critical` |
| `cases list --status` | `open` · `closed` · `all` |
| `rules promote --run-frequency` | `LIVE` · `HOURLY` · `DAILY` |
| `dashboards charts add --chart-type` | `area` · `bar` · `gauge` · `line` · `map` · `metrics` · `pie` · `scatter` · `table` |
| `search export --fields` (column labels) | `timestamp` · `user` · `hostname` · `process name` · `raw log` · `udm.additional.*` |

List a case's valid root-causes with `cases values root-causes`. For any other enum,
read the flag's `enum` array in `commands --json` (or its `--help`).

## Gotchas

### A 500 is usually wrong-host, not a broken endpoint

The same v1alpha paths are served by two hosts. SOAR-flavored surfaces (cases, Content
Hub, connectors) **500 on the chronicle host** and work on the SOAR host; SIEM surfaces
(rules, iocs, riskConfig) **404 on the SOAR host**. The CLI routes correctly — but
before declaring a surface broken, try `--legacy` (forces the legacy AppKey path on
dual-generation surfaces) or `soar legacy call` as an escape hatch. Never retry a
mutating POST that 500s — it may have already applied.

### A search event id needs URL-safe base64

`search event <id>` takes the base64 `metadata.id` from a search result. The enriched
path needs URL-safe, unpadded base64 — the CLI converts it for you; just pass the id
verbatim from `search udm --json` / `--format jsonl`.

### Playbook UUIDs rotate on save

Every save of a SOAR playbook mints a new `identifier`. **Resolve playbooks by name**
(`--name`), not by identifier, and re-read the list after a save.

### Playbook & integration inspection

`playbooks get <name|uuid>` — structure, trigger, step breakdown, integration
deps, block refs. `playbooks lint (--name | --all)` — static analysis: broken
block refs, missing integrations, placeholder-in-JSON, whitespace triggers.
`playbooks health` — fleet-wide run stats sorted by failure rate. `playbooks
diff <name> <local.json>` — unified diff of live vs export. `playbooks duplicate
<name> --name <new>` — guarded clone (native API; falls back to export→save on
server 500). `integrations get <id>` — version, instances, playbook usage.
`integrations test <id>` — connectivity test (PASS/FAIL with error message;
`--instance <id>` for a specific instance). `cases simulation create
--event-field key=value --alert-field key=value` adds UDM fields to simulated
cases. `cases simulation export <name>` — export as JSON; `cases simulation
import <file>` — guarded import from JSON.

### Playbook export / import

Two export formats, each for a different workflow:

| Format | Command | Use case |
| --- | --- | --- |
| JSON (default) | `playbooks export --name <pb> [--out file.json]` | Edit → `push playbook` round-trip; input for `playbooks mold` |
| ZIP bundle | `playbooks export --name <pb> --zip --out file.zip` | Cross-tenant promotion, offline backup, `playbooks import` |

`playbooks import --file <bundle.zip> [--yes]` — guarded import; creates copies
with an `IMPORT N -` prefix, disabled, in the "Imported Playbooks" category.

### Curated rules: browse → search → drill → toggle

The curated workflow is a drill-down: `categories` (12 overview rows) →
`rule-sets` (default enabled only, `--all` for catalog, `--search`/`--category`) →
`rules --set <id>` (individual rules in one set) → `rule <id>` (detail with
resolved names). `curated search <query>` is the unified search across both sets
and rules (`--installed`/`--tactic`/`--severity`). ~80% of rule sets don't expose
individual rules via the API — the CLI shows a helpful message for those.

`curated set` toggles `enabled`/`alerting` scoped to a `--category`/`--ruleset`/
`--precision`. There is no per-rule override for Google-managed curated rules — a
platform limit, not a CLI gap.

### Playbook ZIP bundle schema

The ZIP from `playbooks export --zip` contains one
`<playbook-display-name>.json` per playbook:

```text
├── CategoryName                  string (folder name in the UI)
├── Definition
│   ├── Name, Identifier          string (UUIDs)
│   ├── Steps[]                   action / condition / block steps
│   │   ├── Type                  int (0=ACTION, 4=CONDITION)
│   │   ├── Integration, ActionName, ActionProvider
│   │   └── Parameters[]
│   ├── Triggers[]
│   │   ├── Type                  int (8=ALL, 10=CASE_DATA)
│   │   ├── Conditions[], LogicalOperator (0=AND, 1=OR)
│   │   └── Environments[]
│   ├── IsEnable, IsAutomatic     bool
│   └── Category, Priority, PlaybookType, CreationSource   int enums
├── OverviewTemplatesDetails[]
└── WidgetTemplates[]
```

Integer enums use the server's encoding — run `status enums` for the
SDK-known mappings, `status enums --live` for instance-specific values
(stages, categories). The bundle is self-contained; `playbooks import`
handles the base64 envelope automatically.

### Write-then-list lag; a failed write may have applied

After any create/update: a write can **return an error yet still persist** the object
(verify with get/list — don't assume failure means nothing happened), and create→list
has indexing lag while deleted ids tombstone. Give throwaways unique self-identifying
names (e.g. `secopsctl-smoke-<nanos>`) and **delete by exact id**, never a list sweep.

## Safety rules

- **No mutation without the dry-run review.** Show the preview, then `--yes`.
- **Never commit real identifiers.** Use placeholders (`your-project-id`,
  `000000000000`, `00000000-0000-0000-0000-000000000000`, `your-tenant`,
  `example.com`). The pre-commit hook (`.githooks/pre-commit`) enforces this.
- **No secrets in the repo.** Config lives in git-ignored `~/.secopsctl/instance.yaml`
  (`0600`); tokens are never written to disk. Never commit AppKeys, OAuth tokens, or
  service-account JSON.
- **Clean up throwaways.** Delete by exact id any object created for a smoke/probe;
  `secopsctl cleanup smoke-artifacts` removes secopsctl-owned smoke objects.

## Quick reference

| I want to… | Command |
|---|---|
| Verify setup / config | `secopsctl doctor` · `secopsctl info` |
| Discover commands / surfaces | `secopsctl commands --json` · `secopsctl status surfaces` |
| Hunt events | `secopsctl search udm '<filter>' --format jsonl` |
| Inspect one event / its raw log | `secopsctl search event <id>` · `… --raw` |
| Export all matches to CSV | `secopsctl search export '<filter>' --out hits.csv` |
| Aggregate (stats) | `secopsctl search stats '<agg>'` |
| NL → query (Gemini) | `secopsctl gemini generate '<question>'` · `gemini search '<question>'` |
| Validate a query first | `secopsctl search validate '<filter>'` |
| Save / share a search | `secopsctl search saved save --name <n> --query '<q>' [--share] --yes` |
| Snapshot a surface | `secopsctl pull <target>` · `secopsctl soar pull <target>` |
| Deploy changes | `secopsctl push <target> --dry-run` → `--yes` (SOAR: `soar push`) |
| Triage cases | `cases list` → `cases get <id>` → `cases close --id <n> --reason <r> --yes` |
| Ship a rule | `secopsctl rules promote <file.yaral> --dry-run` → `--yes` |
| Browse curated detections | `secopsctl curated categories` → `curated rule-sets` → `curated rules --set <id>` |
| Search curated catalog | `secopsctl curated search <query> [--installed] [--tactic T]` |
| Toggle curated rules | `secopsctl curated set --category <c> --ruleset <r> --precision broad --dry-run` |
| Install Content Hub content | `secopsctl content-hub install --identifier <id> --dry-run` |
| Pivot an investigation | `secopsctl entities graph <detection-id>` · `secopsctl entities risk-scores` |
| Recover from ADC lapse | `gcloud auth login` then `secopsctl doctor` |
| Force legacy SOAR path | add `--legacy` |
| Hard read-only for an agent | `SECOPS_READONLY=1 secopsctl ...` |
| Re-read this guide | `secopsctl skill` |

## This skill

`secopsctl skill` prints this guide from the binary (`--json` for `{name, description,
body}`). `secopsctl skill install [--dir <skills-dir>]` writes it into an agent skills
directory (default `$CLAUDE_CONFIG_DIR/skills` or `~/.claude/skills`) so the harness
detects it as a first-class skill — do this once the user approves.

Deeper references live in the repo and at **secops.danny.vn** (an install-only agent
without the repo should lean on the self-discovery commands above):

- `docs/guides/the-loop.md` — the pull → diff → push walkthrough
- `docs/guides/search.md` — the full search surface (udm/raw/stats/event/export/validate/saved)
- `docs/guides/gemini.md` — NL→query generation and the Gemini assistant
- `docs/guides/triage.md` — the SOC triage loop (queue → case → verdict → act → tune)
- `docs/guides/content-hub.md` — browse, install, and uninstall Content Hub content
- `docs/guides/reference-siem.md` / `reference-soar.md` — the complete command reference
- `docs/design/catalog.md` — live status of every surface (designed / built / validated)
- `docs/tips/11-gemini-and-ai.md` — agent allowlists, the audit log, AI-driven recipes
