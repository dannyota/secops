# Changelog

Notable changes per release. Earlier releases (v0.1.x – v0.2.x) carry their
notes in the signed tag messages.

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

  The mutating verbs are guarded (dry-run by default, `--yes` to apply). Validated
  end to end by `TestLiveDashboardsChartCLISmoke`. A lossless deref-on-pull
  round-trip (capturing chart queries into the mirror) remains a tracked follow-up.

## v0.4.2 — 2026-06-22

### Added

- Native-dashboard **chart-query authoring** is live-validated. A dashboard's
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
  the engine surface. Live-validated: pulled dashboards carry no actor ids and
  `drift dashboards` is in sync.

## v0.4.0 — 2026-06-12

Operator-confidence fixes from dogfooding the config-as-code loop, plus a new
alert-grouping reconcile surface.

### Added

- `soar push grouping` — alert-grouping rules as config-as-code via the modern
  v1alpha `alertGroupingRules` API (siemplify-soar host). Reconciles
  create/update/delete; `--prune` deletes server-only rules but refuses the
  non-deletable catch-all fallback (`category: ALL`). Validated end to end against
  a live instance.
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
