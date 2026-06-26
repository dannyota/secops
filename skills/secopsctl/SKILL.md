---
name: secopsctl
description: >
  Operating guide for AI agents driving the secopsctl CLI against a Google
  SecOps instance (Chronicle SIEM + Siemplify SOAR). Encodes the two-auth-plane
  model, the mutation ritual, the config-as-code loop, self-discovery commands,
  end-to-end recipes, the enum values, and the gotchas the per-command --help
  can't express. Self-served: `secopsctl skill` prints it from the binary. Read
  this before issuing any secopsctl command.
---

# secopsctl agent operating guide

secopsctl operates a Google SecOps instance — **Chronicle SIEM** and **Siemplify
SOAR** — as code. This guide makes you productive without the repo docs; the live
commands (`commands --json`, `surfaces`, `<cmd> --help`) are the source of truth
when something here looks out of date.

## Session bootstrap — do these first

```bash
secopsctl doctor                # config + auth + SIEM/SOAR reachability (read-only)
secopsctl capabilities --json   # version, per-surface status, auth health per plane, read-only state
secopsctl commands --json       # every verb: path, kind (read/guarded-mutation), flags, --json support
```

`doctor` is the gate: if either plane reports unhealthy, fix auth before proceeding.
`capabilities --json` and `commands --json` are the **live source of truth** for what
this binary supports — prefer them over any static list; surfaces and flags evolve.

## The two auth planes

Every command runs against one of two **independent** credential planes — a SOAR
AppKey call works even when SIEM ADC is expired, and vice-versa.

| Plane | Host | Auth | Commands |
|---|---|---|---|
| **SIEM** (Chronicle) | `{region}-chronicle.googleapis.com` | Google ADC / OAuth (minted in-process, never on disk) | `pull`, `push`, `drift`, `rules`, `alerts`, `curated`, `query`, `entity`, `iocs`, `ti`, `feeds`, `parsers`, `dashboards`, `watchlists`, `cases`, `pipeline` |
| **SOAR** (Siemplify) | `{tenant}.siemplify-soar.com` | AppKey (`soar_app_key` in config or `$SECOPS_SOAR_APP_KEY`; no ADC) | `soar pull/push`, `soar case`, `soar playbook`, `soar job`, `soar integration`, `soar marketplace`, `soar settings`, `soar legacy` |

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

## The mutation ritual — every guarded verb

`pull`/`drift`/`list`/`get`/`query` are read-only. Every verb that changes live state
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
`secopsctl surfaces` (PRUNE column) or `push <target> --help`.

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
`secopsctl surfaces` lists each surface's lane and `--prune` eligibility.

## Command self-discovery

Do not guess command names, flags, or enums — read the live catalog:

- `secopsctl commands --json` — every verb: `path`, `kind`, per-flag `{type, default, required, enum, usage}`, `json` support, an example.
- `secopsctl capabilities --json` — version, auth health per plane, per-surface status (validated vs blocked), read-only state.
- `secopsctl surfaces [--json]` — every API surface family: plane, version, lane, status, `--prune` eligibility.
- `<cmd> --help` — per-command flags plus plane/version/write gotchas.

Build an allowlist by filtering `commands --json`: `kind == "read"` is always safe;
`kind == "guarded-mutation"` needs the dry-run → `--yes` ritual.

## Output discipline

Pass `--json` on any read command for parseable output (the human table is for people).
Under `--json`, a failure prints a structured `{code, message, retryable, status,
request_id}` envelope on **stderr** while stdout stays clean for the payload — so
branch on exit code, parse stdout, surface stderr.

## Common recipes

End-to-end, copy-pasteable. Replace placeholders; preview before `--yes`.

### Search UDM events

```bash
secopsctl query udm 'metadata.event_type = "NETWORK_CONNECTION"' --hours 6 --json
secopsctl query udm 'principal.hostname = "host-01"' --from 2024-01-02T00:00:00Z --to 2024-01-03T00:00:00Z
```

Pull raw logs for a broken/missing parser (events normalize to `GENERIC_EVENT`), pipe
straight into a parser test:

```bash
secopsctl query udm 'metadata.log_type = "KONG_GATEWAY" AND metadata.event_type = "GENERIC_EVENT"' \
    --raw --limit 50 | secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -
```

### Aggregate (stats) — the YARA-L a dashboard chart uses

`query udm` rejects an aggregation; `query stats` runs it. `match:` declares the
group-by, `outcome:` the measures, `order:` the sort:

```bash
secopsctl query stats --hours 24 'metadata.log_type != ""
match: metadata.log_type
outcome: $c = count(metadata.id)
order: $c desc'
```

A free-standing `query stats` takes a **bare field** in `match:` (`metadata.log_type`),
not an assignment (`$lt = …`); the `outcome:` declares the measures. Validate a chart
query with `query stats` **before** `dashboards add-chart`. Don't name a `match:`/
`outcome:` variable with a reserved YARA-L keyword (e.g. `$rule`, `$events`) — it
compiles but fails at execute time; `add-chart`/`edit-chart` warn when you do.

### SOC triage — queue → case → AI verdict → close

```bash
secopsctl soar case list --status open --sort priority --json     # the queue, worst first (table adds an SLA column)
secopsctl soar case aging --limit 20                              # oldest open cases + SLA status
secopsctl soar case workload                                      # open-case load per analyst
secopsctl soar case get <id> --json                               # case + alerts (+ firing rule per alert)
secopsctl soar case overview --id <id>                            # entities + enrichment behind the Overview tab
secopsctl soar case summarize --id <id>                           # Google AI summary: summary/reasons/next steps
secopsctl soar case run-action --id <id> --action <name> --instance <uuid> --dry-run  # run an integration action
secopsctl soar case close --id <id> --reason not-malicious \
    --root-cause '<your-root-cause>' --comment 'false positive' --yes
```

Bulk: `soar case assign|tag|stage --ids 1,2,3` acts on a set in one call; `soar case stats`
gives open/closed counts + age/resolution percentiles. Per-**alert** triage (close one alert
without closing the case): add `--alert <alert-id>` to `close`, or `soar case alert <verb>`.
Sort the alert queue too: `secopsctl alerts list --sort priority`.

### Ship and tune a detection rule

```bash
secopsctl rules test detections/new-rule.yaral --hours 24          # PREVIEW detections vs history (no deploy)
secopsctl rules promote detections/new-rule.yaral --dry-run        # validate + create + deploy in one step
secopsctl rules promote detections/new-rule.yaral --alerting=false --yes
# tune existing tracked rules (enabled/alerting/frequency reconcile):
secopsctl pull rules && git diff && secopsctl push rules-deploy --dry-run
secopsctl rules trends --hours 168                                 # noisiest rules, to drive tuning
secopsctl coverage                                                 # MITRE ATT&CK coverage posture
```

Investigate: `secopsctl entities graph <detection-id>` walks the findings-graph pivot;
`secopsctl entities risk-scores --order-by 'riskScore desc'` ranks hosts/users.

### Author a dashboard chart

```bash
secopsctl pull dashboards --with-charts                            # inline charts + queries
secopsctl dashboards add-chart <dash-id> --title 'Top log types' \
    --query-file q.yaral --chart-type bar --x '$lt' --y '$c' --dry-run
secopsctl dashboards run-chart <dash-id> --chart-id <c>            # execute, read the rendered rows
secopsctl dashboards verify <dash-id>                              # flag empty/errored charts (exit 2 on any)
```

`--x/--y/--series-by` are validated against the query's `match:`/`outcome:` variables,
so a typo fails clean instead of producing a blank chart.

### Reconcile any surface (generic)

```bash
secopsctl soar pull connectors
git diff
secopsctl soar push connectors --prune --dry-run                  # --prune deletes live objects with no local file
secopsctl soar push connectors --yes
secopsctl soar pull connectors
```

## Enums & values you'll need

The CLI takes **names**, not the server's magic ints. Common sets:

| Where | Valid values |
|---|---|
| `soar case close --reason` / `push bulk-close` | `malicious` · `not-malicious` · `maintenance` · `inconclusive` · `unknown` |
| `soar case list --priority` | `informative` · `low` · `medium` · `high` · `critical` |
| `soar case list --status` | `open` · `closed` · `all` |
| `rules promote --run-frequency` | `LIVE` · `HOURLY` · `DAILY` |
| `dashboards add-chart --chart-type` | `bar` · `line` · `pie` · `table` |

List a case's valid root-causes with `soar case values root-causes`. For any other
enum, read the flag's `enum` array in `commands --json` (or its `--help`).

## Gotchas

### A 500 is usually wrong-host, not a broken endpoint

The same v1alpha paths are served by two hosts. SOAR-flavored surfaces (cases, Content
Hub, connectors) **500 on the chronicle host** and work on the SOAR host; SIEM surfaces
(rules, iocs, riskConfig) **404 on the SOAR host**. The CLI routes correctly — but
before declaring a surface broken, try `--legacy` (forces the legacy AppKey path on
dual-generation surfaces) or `soar legacy call` as an escape hatch. Never retry a
mutating POST that 500s — it may have already applied.

### Playbook UUIDs rotate on save

Every save of a SOAR playbook mints a new `identifier`. **Resolve playbooks by name**
(`--name`), not by identifier, and re-read the list after a save.

### Curated rules: toggle is set×precision only

`curated set` toggles `enabled`/`alerting` scoped to a `--category`/`--ruleset`/
`--precision`. There is no per-rule override for Google-managed curated rules — a
platform limit, not a CLI gap.

### Pull-time secret redaction

`pull` masks values matching a `.secopsctl-redact` file at the data root (or
`soar pull --redact <regex>`) to `***REDACTED***`. It is drift-safe (pull/drift/push
load the same patterns). A push of a body still carrying the marker is **refused** —
restore the real value or reference an env/credential first. Never hand-edit the marker.

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
| Discover commands / surfaces | `secopsctl commands --json` · `secopsctl surfaces` |
| Snapshot a surface | `secopsctl pull <target>` · `secopsctl soar pull <target>` |
| Deploy changes | `secopsctl push <target> --dry-run` → `--yes` (SOAR: `soar push`) |
| Triage cases | `soar case list` → `soar case get <id>` → `soar case close --id <n> --reason <r> --yes` |
| Ad-hoc UDM search / aggregate | `secopsctl query udm '<filter>'` · `secopsctl query stats '<agg>'` |
| Preview a rule before deploy | `secopsctl rules test <file.yaral> --hours 24` (detections, no deploy) |
| Ship a rule | `secopsctl rules promote <file.yaral> --dry-run` → `--yes` |
| Pivot an investigation | `secopsctl entities graph <detection-id>` · `secopsctl entities risk-scores` |
| Queue metrics | `soar case aging` · `soar case workload` · `soar case stats` |
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
- `docs/guides/triage.md` — the SOC triage loop (queue → case → verdict → act → tune)
- `docs/guides/playbooks.md` — discover, author, operate SOAR playbooks
- `docs/guides/usage.md` — the complete command reference, every flag
- `docs/guides/soar-cases.md` — per-case and per-alert verb reference
- `docs/design/catalog.md` — live status of every surface (designed / built / validated)
- `docs/tips/10-llm-and-automation.md` — agent allowlists, the audit log, automation recipe
