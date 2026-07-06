# Changelog

Notable changes per release. Earlier releases (v0.1.x – v0.2.x) carry their
notes in the signed tag messages.

## v0.9.5 — 2026-07-07

### Fixed

- **MCP: focus output hints the callable tool name prefix.** The focused
  tool list now shows `(callable as mcp__<server>__<name>)` so agents
  know the full tool name to use.

## v0.9.4 — 2026-07-07

### Added

- **Playbook lint: detect step relation cycles.** `playbooks lint` now
  detects cycles in the step relation graph (`[error] cycle`) — a
  playbook with a cycle may loop infinitely at runtime.

## v0.9.3 — 2026-07-06

### Added

- **Playbook lint: detect dangling step relations and orphan steps.**
  `playbooks lint` now flags orphaned `stepsRelations` entries whose
  `fromStep` or `toStep` references a step that no longer exists
  (`[error] dangling-relation` — causes console 500 on edit), and steps
  with no incoming relation (`[warning] orphan-step` — disconnected from
  the flow). When dangling relations are found, the fix instructions name
  the affected indices and orphan steps to reconnect.

## v0.9.2 — 2026-07-06

### Fixed

- **MCP: `run` respects quoted arguments.** The `run` meta-tool split on
  whitespace, breaking flag values with spaces (e.g.
  `--name "SOC Agents - Auto-Trigger"`). Now uses a shell-style tokenizer
  that respects single and double quotes.

## v0.9.1 — 2026-07-06

### Fixed

- **MCP: hyphenated tool names now callable.** Focused tool names like
  `playbooks_debug-step-data` contained hyphens that MCP clients normalize to
  underscores — the server rejected the underscore form. Tool names are now
  all-underscore (`playbooks_debug_step_data`); reverse resolution to cobra
  commands already handled this.
- **Playbook run/rerun: auto-resolve alertGroupIdentifier from the case.**
  `playbooks run` and `playbooks rerun` now fetch the case detail and extract
  the opaque `alertGroupIdentifier` when `--alert-group` is omitted. Previously
  the API 500'd because the required identifier was missing or callers passed the
  human-readable `alert.identifier` (from `cases get`) instead of the internal
  value from `alerts[].additionalProperties.alertGroupIdentifier`.

## v0.9.0 — 2026-07-06

### Changed

- **MCP: progressive discovery replaces flat tool list.** Initial `tools/list`
  now returns 5 meta-tools (`run`, `help`, `usage`, `focus`, `unfocus`) instead
  of ~50+ individual tools. Agents discover commands via `help`, inspect one
  command's schema via `usage`, execute via `run`, or load a full group's typed
  tools via `focus`/`unfocus` (dynamic `listChanged` notifications).
- **MCP: grouped command catalog.** `help` returns a compact catalog (~4.5K
  chars) with group name, description, and command counts. Groups with
  sub-commands (soar, ingest, playbooks, cases, dashboards, integrations,
  rules, data-access) auto-show a sub-group catalog; drill deeper with
  dotted notation (e.g. `help group=soar.push`).
- **MCP: server instructions.** The `initialize` response includes instructions
  guiding agents through the help → usage → run discovery flow.

### Improved

- **`commands` accepts a group argument.** `commands <group>` drills into one
  group; `commands` (no arg) shows the grouped catalog. Drill-down JSON drops
  flags (agents use `usage` instead) — worst case 21K → 4.7K chars.

## v0.8.6 — 2026-07-06

### Improved

- **MCP argument schemas.** Flag-only commands no longer emit a bogus `args`
  property. The `stripFlagHints` pipeline (balanced-bracket scanner + bare-flag
  regex + residue cleaner) correctly isolates genuine positional args from
  flag documentation in cobra `Use` strings — fixes ~85 commands.
- **Command descriptions.** All `Short` strings rewritten verb-first for
  cleaner MCP tool descriptions, `commands --json` catalog, and auto-generated
  docs. Normalized 16 `GUARDED:` prefixes to `MUTATING (guarded):`.

### Added

- **`parsers deactivate`** — revert a custom parser to the prebuilt version
  (auto-selects the ACTIVE CUSTOM parser).
- **`parsers activate`** — activate a parser by log-type alone (auto-selects
  the latest INACTIVE CUSTOM version).
- **`parsers versions`** — now shows validation stage, version, and release
  stage columns.

### Fixed

- **Parser create** retries activation on `FAILED_PRECONDITION` and handles
  `VALIDATION_SKIPPED` / `INTERNAL_ERROR` stages.
- **Dashboard `charts add`** binds new charts to `GlobalTimeFilter` via
  dashboard PATCH after creation.
- **Playbook save** coerces integer step types to string enums and warns when
  SOAR silently drops steps.
- **Playbooks `create`** strips identity fields to prevent overwriting source.
- **Search view** shows elapsed-time progress during blocking fetch.

### Docs

- Removed `catalog-siem.md` / `catalog-soar.md` detail files (duplicated
  auto-generated command docs); compact status matrix in `catalog.md` stays.

## v0.8.5 — 2026-07-06

### Added

- **`playbooks attach` alias.** `playbooks attach` is now an alias for
  `playbooks run`, matching the console's "Attach playbooks manually" action.
- **`playbooks run/attach` name→UUID auto-resolution.** `--name` now
  auto-resolves the `originalWorkflowDefinitionIdentifier` (same two-step
  resolution as `rerun`), sends both `wfName` and the uuid, and includes
  `inputParameters: []` — matching the console's request shape exactly.
  The `--automatic` flag is removed (console does not use it).

## v0.8.4 — 2026-07-06

### Fixed

- **`playbooks rerun` 500 fix.** The rerun command was sending `wfName`
  (display name) but the API requires `originalWorkflowDefinitionIdentifier`
  (the immutable original playbook uuid). `--name` now auto-resolves via
  list → get → extract `originalPlaybookIdentifier`. Dropped the unused
  `shouldRunAutomatic` field and `--automatic` flag from the rerun body.

## v0.8.3 — 2026-07-06

Progressive tool disclosure for the MCP server — reduces the initial tool
listing from 361 tools / 207K chars to 36 tools / 41K chars.

### Changed

- **MCP progressive disclosure.** `mcp serve` now declares `listChanged`
  capability and uses a three-tier tool hierarchy. Tier 0: standalone commands
  and promoted high-use tools (search_udm, gemini_ask, etc). Tier 1: category
  routers — one tool per top-level group whose description summarizes available
  subcommands. Tier 2: sub-tools registered on demand when a category is first
  called, via MCP `notifications/tools/list_changed`. Calling a category with
  `action="help"` returns the full subcommand listing.

## v0.8.1 — 2026-07-06

MCP-first agent integration. The `skill` command is removed; agent integration
is now `secopsctl mcp install` → every Claude Code session gets all 361
secopsctl tools and 17 craft-guide resources automatically.

### Added

- **MCP server.** `mcp serve` — Model Context Protocol server over stdio
  JSON-RPC. Auto-generates tools from the cobra command tree with typed
  InputSchema (flags → JSON schema properties, positional args, enums).
  Guarded mutations carry `[guarded: dry-run by default]` in the description.
  Zero external dependencies.
- **MCP resources.** All `docs/tips/*.md` files embedded in the binary and
  served as `tips://{name}` resources — recipes, gotchas, the auth model,
  search surface, parser extensions.
- **`mcp install`.** Registers secopsctl in `.claude/settings.json`
  (`--global` for `~/.claude/settings.json`).
- **Tips gap-fill.** `15-recipes.md` (cross-cutting workflows) and
  `16-gotchas.md` (operational traps). Tips embed expanded from single-file to
  `embed.FS` with `All()` export.

### Removed

- **`skill` / `skill install`** and the `skills/secopsctl/` package — fully
  replaced by `mcp serve` + MCP resources.

### Changed

- `status capabilities` JSON: `skill_command` → `mcp_command`.

## v0.5.1 — 2026-06-26

Dashboard portability, a quota-aware transport, and a broad operator command
suite that wires SDK surfaces the CLI had not yet exposed. Every mutation stays
behind the standard dry-run/`--yes` guard with a structured `--json` result.

### Added

- **Dashboard portability.** `dashboards export <id>` / `import <file>`
  round-trip a dashboard with its charts and queries as one JSON document.
  `dashboards duplicate` uses the server `:duplicate` verb (the copy gets its own
  independent charts and queries) with a `--deep-copy` client-side fallback.
  `cases overview` surfaces a case's entities and the overview widget template.
- **Detection lifecycle.** `rules test <file.yaral>` dry-runs a rule against
  historical data (preview detections, no deploy); `rules versions` (+ `--show`);
  `rule-exclusions list|get`; `rules retrohunt create --wait`; `coverage`
  (MITRE ATT&CK).
- **Investigation.** `entities graph <detection-id>` (findings-graph pivot) and
  `entities risk-scores`.
- **Triage.** `alerts list --sort`, `cases list --sort` + an SLA column,
  `cases workload|aging|stats`, and bulk `cases assign|tag|stage --ids`.
- **Case work.** `cases run-action` (run an integration action on a case),
  `cases task`, and `cases evidence`.
- **RBAC / data.** `data-access labels|scopes` CRUD; `watchlists
  create|delete|remove-entity`.
- **SOAR ops.** `soar connector stat|run`.
- **Platform / data.** `log-types`, `forwarders` (+ collectors),
  `feeds list|get|service-account`, and `ingestion health`.

### Changed

- **Quota-aware transport.** The chronicle and SOAR clients honor a 429's
  `Retry-After` / `RetryInfo`, add equal jitter and a total backoff budget, plus an
  opt-in client-side rate limiter (off by default); the CLI prints a clear hint
  when retries exhaust. A non-idempotent POST is not retried on a 5xx.
- `alerts enrich` reads the collection that backs the per-alert context view
  (rule, UDM events, entities/indicators, MITRE tags, triage, case bridge).
- Chart reads (`dashboards charts`, `pull --with-charts`, the copy path) use one
  `dashboardCharts:batchGet` with a per-chart fallback; `dashboards verify` treats
  a 4xx chart failure as broken and a 429/5xx as transient.
- MCP resources serve the domain knowledge formerly in the bundled agent skill.

## v0.5.0 — 2026-06-25

An operator-experience and agent-enablement milestone (Waves 73–83): the CLI's
own surface becomes machine-discoverable, daily triage scales, deploy previews
get more honest, the last config-as-code fidelity edges close, and dashboard
authoring gains a verify half. Every mutation stays behind the standard
dry-run/`--yes` guard.

### Added

- **Agent enablement.** `secopsctl capabilities [--json|--offline]` — one
  session-bootstrap call fusing version, per-plane auth health, read-only state,
  and a surface-status rollup (validated vs blocked). `commands --json` now
  carries per-flag `{type, default, required, enum, usage}`, positional-arg spec,
  and an example per command. A failed command under `--json` emits a structured
  `{code, message, retryable, status, request_id}` envelope on stderr (so stdout
  stays clean for the payload). `push --json` dry-run includes a per-object change
  plan (`items[]`).
- **Agent enablement via MCP.** `secopsctl mcp serve` auto-generates MCP tools
  from the command tree; `mcp install` registers in Claude Code settings.
- **Query library.** `query run --file <path>|-` and `query saved [<name>]` run a
  UDM predicate from a file/stdin or from a tracked `saved_queries/` pack.
- **Alert triage at scale.** `alerts update --where <filter>` / `--stdin-ids`
  apply one update to many alerts in a reviewed, guarded command; `alerts list`
  surfaces a completeness signal (baseline count + a truncation warning — the
  alerts snapshot has no server cursor).
- **Bulk case close from a filter.** `soar push bulk-close --where <filter>`
  selects cases by a modern cases-list filter with a typed close-reason.
- **Rule promote.** `rules promote <file.yaral>` validates, creates, and deploys
  a new rule in one guarded step.
- **Curated blast-radius preview.** `curated set` previews the addressed
  deployment's current → requested state and the set×precision scope before the
  guard.
- **Dashboard reconcile completion.** A schema-checked dry-run (an API-invalid
  body now fails the preview, not just at `--yes`); chart layout / filters /
  reorder / removal reconcile through `push`; `pull --with-charts` reports the
  count of charts that degraded to a reference.
- **Grouping settings.** `soar settings grouping get` reads the alert-grouping
  General/Overflow settings singleton (the legacy max-alerts value plus any modern
  moduleSettings properties); guarded `set` writes where the instance exposes
  writable properties (the max-alerts singleton is read-mostly — no API SET).
- **Query stats.** `query stats '<match:/outcome: YARA-L>'` runs an aggregation
  query (which `query udm` rejects) and prints the result table — validate a chart
  query before authoring it.
- **Dashboard chart execute & verify.** `dashboards run-chart`/`values` prints the
  values a chart renders; `dashboards verify` flags empty/errored charts (a
  headless dashboard health check) — the missing verify half of authoring.
- **Chart-type authoring.** `add-chart`/`edit-chart --chart-type bar|line|pie|table
  --x --y [--series-by]` generate + validate the visualization (vs hand-writing
  echarts JSON); `edit-chart` edits visualization/layout in place; `add-charts
  --file` batch-authors a dashboard (paced, idempotent).

### Changed

- The blocked Chronicle-host case verbs (`cases list/get/search`) are hidden from
  help (still runnable for when the endpoint stabilizes); the working `cases
  soar-id` uuid→id bridge stays visible.

## v0.4.5 — 2026-06-22

### Fixed

- Default chart layout for `dashboards add-chart` (and the reconcile fallback when
  an authored chart omits its layout) is now full-width on the native-dashboard
  **96-column grid** (`spanX: 96`, `spanY: 16`) instead of the previous `spanX: 12`,
  which rendered a chart at one-eighth of the width. `chartLayout` `startX`/`spanX`
  range over `0–96`. Explicit `--layout` values are unaffected.

## v0.4.4 — 2026-06-22

### Added

- `pull dashboards --with-charts` dereferences each chart into its inline YARA-L
  query so a dashboard round-trips as code (the dashboard body only references
  charts; the query lives in separate `dashboardCharts`→`dashboardQueries`
  resources). The default `pull dashboards` stays reference-only — a handful of
  requests and deterministic `drift` — while `--with-charts` does the heavier
  per-chart deref; a chart that can't be fetched standalone (some 404) or that hits
  the per-minute quota (429) degrades to a reference rather than losing the
  dashboard. `push` and `drift` detect an inline mirror and dereference the live
  side to match (so an inline mirror never phantom-diffs a reference-only live).

### Changed

- `push dashboards` of an inline mirror reconciles charts: a new chart via
  `:addChart`, a changed query / title / visualization / drilldown via `:editChart`
  (etag-guarded). Chart layout/filters/datasource edits, reordering, and removal are
  reported rather than applied — edit layout in the UI and remove charts with
  `dashboards remove-chart`. Per-chart server ids are kept out of the diff basis.

## v0.4.3 — 2026-06-22

### Added

- `dashboards` chart-authoring CLI verbs surface the Wave 70 SDK ops so a charted
  dashboard can be built from the command line:
  - `dashboards add-chart <id> --title <t> --query <yaral>` (or `--query-file`) adds
    a chart and its YARA-L query in one `:addChart` call; `--layout`/`--datasource`/
    `--interval`/`--tile-type` default sensibly and JSON flags are validated up front.
  - `dashboards edit-chart <id> --chart-id <c> --query <yaral>` replaces a chart's
    query via `:editChart`, round-tripping the query's etag.
  - `dashboards remove-chart <id> --chart-id <c>` removes a chart.
  - `dashboards charts <id>` lists each chart with its resolved query (read-only;
    `--json`), the way to recover a `--chart-id` or review what a dashboard runs.

  The mutating verbs are guarded (dry-run by default, `--yes` to apply). A lossless deref-on-pull
  round-trip (capturing chart queries into the mirror) remains a tracked follow-up.

## v0.4.2 — 2026-06-22

### Added

- Native-dashboard **chart-query authoring** via the dedicated chart ops. A dashboard's
  `definition.charts[]` is reference-only by API design — each entry references a
  `dashboardCharts` resource by name, and the YARA-L query lives one hop further in
  a `dashboardQueries` resource — so `push dashboards` (wholesale `definition.charts`
  replace) re-points chart references and layout but cannot author a chart query.
  The query is authored through the dedicated chart ops the SDK carries:
  `AddChart` (`:addChart`, with an inline `dashboardQuery{query,input}`),
  `EditChart` (`:editChart`, etag-guarded), `RemoveChart`, `GetChart`, `GetQuery`.
  Proven end to end by `TestLiveDashboardChartAuthoringWriteSmoke` (create →
  add chart with a YARA-L query → confirm the query round-trips → confirm the
  dashboard definition stays reference-only → edit the query → remove → delete),
  with offline tests pinning the request shapes. Surfacing these as a CLI command
  and dereferencing chart bodies on `pull dashboards` are tracked follow-ups.

## v0.4.1 — 2026-06-15

### Fixed

- Server-managed actor ids (`createUserId` / `updateUserId`) are now stripped
  GLOBALLY by the reconcile engine (alongside the time fields), instead of as a
  `dashboards`-only special case, so no config-as-code surface can write a tenant
  user id into a committed file (and `updateUserId` no longer churns the diff on
  every edit). Removed the dead, leaky `PullDashboards` puller (no caller; it wrote
  raw CURATED dashboard list items including `createUserId`); `pull dashboards` uses
  the engine surface. Pulled dashboards carry no actor ids and `drift dashboards`
  is in sync.

## v0.4.0 — 2026-06-12

Operator-confidence fixes from dogfooding the config-as-code loop, plus a new
alert-grouping reconcile surface.

### Added

- `soar push grouping` — alert-grouping rules as config-as-code via the modern
  v1alpha `alertGroupingRules` API (siemplify-soar host). Reconciles
  create/update/delete; `--prune` deletes server-only rules but refuses the
  non-deletable catch-all fallback (`category: ALL`).
- Pull-time value redaction: a committed `.secopsctl-redact` patterns file (one
  regex per line) at the data root, plus an ad-hoc `--redact` flag on `soar pull`,
  masks secrets that arrive as plain inline strings (e.g. a webhook URL carrying a
  token in a playbook step parameter). Drift-safe — pull, drift, and push load the
  same patterns, so a masked value never produces a phantom diff; a marker guard
  refuses to push a body still carrying the redaction marker.
- `drift` now names the diverged objects — `[+a ~b -c]` in the text report and
  `created_names` / `updated_names` / `deleted_names` / `untracked_names` in
  `--json` — so a bare count is diagnosable.

### Fixed

- `push rules-deploy` field-masks the deployment PATCH to only the fields that
  differ from live, so an alerting-only flip no longer trips a 409 on an unchanged
  `enabled`; a residual "already enabled/disabled" 409 is treated as
  success-with-note; and the summary reports deployed / already-in-desired-state /
  failed truthfully, exiting non-zero only on a genuine failure.
- `drift reference_lists` no longer phantom-reports an empty reference list as
  changed immediately after a clean pull (entries canonicalize to `[]` on both
  the live and on-disk sides).
- `entity summarize` decodes counters the API renders as JSON strings (proto3
  int64), fixing the decode error on `alertCounts.count` and the sibling
  prevalence/timeline/widget counters.

### Changed (migration)

- `soar playbook export` (default JSON) now emits the save-compatible definition —
  the same shape `pull playbooks` writes — so the export → edit → `push playbook`
  loop round-trips and the file is what `soar playbook mold` / `build-playbook`
  consume. The platform bundle is unchanged on `--zip`. Anything that parsed the
  previous (save-incompatible) export JSON should adopt the new shape.
- `soar pull grouping` writes `grouping/rules/*.json` (full reconcilable config)
  plus the General/Overflow settings singleton at `grouping/settings.json`,
  replacing the earlier lossy `rules/*.yaml` and empty `settings.yaml`. Re-pull
  existing grouping mirrors.

## v0.3.1 — 2026-06-12

Network-layer fix: `force_ipv4` now applies to every outbound connection.

- One shared `auth.HTTPTransport` builds all five outbound HTTP clients —
  the Chronicle SIEM client, the SOAR transport (modern + legacy), in-process
  OAuth token minting, Secret Manager `secret_ref` reads, and the `info cron`
  heartbeat checker — with `http.DefaultTransport`-equivalent pooling and
  timeouts, and the dialer pinned to IPv4 when `force_ipv4` /
  `SECOPS_FORCE_IPV4` is on.
- The `info cron --heartbeat-status` client previously ignored the flag. It
  now reads it validation-free, so a partial config (`force_ipv4` set but
  SIEM keys absent) still pins the dialer, and `info cron` keeps working
  with no config file at all.

## v0.3.0 — 2026-06-11

The operational release: the alert → case → rule triage loop, the AI-assist
layer, the playbook authoring palette, and agent-safety guardrails.

### Triage loop

- `alerts update` — guarded SIEM alert disposition (status / verdict /
  priority / comment), with id fan-out.
- Alert ⇄ case bridges: `alerts get` resolves the SOAR case id;
  `cases soar-id` bulk-resolves SIEM case UUIDs.
- `soar case list` triage filters (`--assignee` / `--priority` / `--tag` /
  `--since`) plus a verbatim server-side `--filter`; the full modern filter
  grammar (scalars, `any()` collections, epoch-ms ranges) is documented and
  supported end to end — long filters auto-switch to the method-override
  POST, zero-match queries decode as empty results.
- `soar case counts` — per-priority queue numbers via the list's `totalSize`.
- Per-alert verbs inside a case: `soar case alert close | priority | move |
  reopen`; case verbs gain `priority`, `reopen`, `comment add/list`.
- Rule tuning reads: `rules trends | counts | events` and `curated
  detections | trends | events` (plus a gated batch rule update in the SDK).

### AI assist

- `alerts investigate <id> [--latest]` — the per-alert Gemini investigation:
  verdict, confidence, summary, suggested next steps (with the agent's UDM
  queries under `--json`); `--latest` is the read-only variant.
- `soar case summarize` — the structured Gemini case summary (poll-first;
  `--refresh` to regenerate).
- `query gemini` — environment-grounded SecOps Gemini chat.
- `soar playbook generate` — AI playbook drafting (returns the draft without
  persisting; instances may restrict the Playbook Assistant to interactive
  auth, surfaced plainly).

### Playbooks

- The authoring palette: `soar playbook components actions` (every action
  across every integration, with numeric ids), `flow` (transformers +
  logical operators), `triggers` (the trigger vocabulary), `blocks`;
  `components usage` resolves an action by name for impact analysis.
- Lifecycle completion: `versions`/`restore`, `stats`, `export`/`import`,
  `step skip`, batch `delete`, and job-instance schedule management.
- `soar playbook step insert` — splice a brand-new action step into a
  playbook graph offline (fresh identity, rewired relations), reviewable
  before save.
- Custom Python definitions over the API: `soar integration action | job-def
  template | create | update | delete` (the IDE's authoring flow, guarded —
  create is POST, update is a sparse PATCH by id).

### Agent safety

- Hard read-only mode: `--read-only` / `SECOPS_READONLY=1` degrades every
  guarded mutation to a dry-run and refuses AI generations.
- `secopsctl commands` — the offline, machine-readable verb catalog
  (read vs guarded-mutation), for building automation allowlists. Each row now
  also reports per-command `--json` support (the `json` field / `JSON` column),
  so agents read it from the catalog instead of a hand-maintained doc list.
- A local JSONL audit log of confirmed mutations (`$SECOPSCTL_HOME/audit.jsonl`).

### SOAR administration

- `soar settings api-keys create | revoke` alongside `list` — the key value
  is minted locally (crypto/rand), shown exactly once, never logged.

### CLI UX

- The root `--help` now renders titled command groups (Setup & health / Read &
  query / Config as code / SOAR / Utilities), a getting-started pointer
  (`config` → `doctor` → `commands`/`surfaces`), and Example blocks on the
  high-traffic commands; a mistyped command suggests the nearest match.
- Sharper errors: invalid enum flags echo your verbatim input and list the
  valid set, `curated --precision` validates before the live-deploy banner,
  `--hours <= 0` is rejected up front, and missing files name the flag.
- Flag spellings aligned (old names kept as hidden aliases): `alerts list
  --filter` (was `--query`), `soar case run-action`/`context set --id` (was
  `--case-id`), `soar integration uninstall --key` (was `--name`). Flag value
  placeholders no longer render stray tokens.

### Fixes

- Watchlist entity writes send the UDM Entity envelope (the previous flat
  noun was rejected); `RemoveWatchlistEntity` removes by resource name.
- AI case summaries no longer restart generation on every read (poll-first).
- int64-bearing SOAR bodies (step skip, job instances, definition authoring)
  round-trip through raw-JSON overlays, immune to float64 truncation.

New guides: [triage](docs/guides/triage.md) ·
[playbooks](docs/guides/playbooks.md).
