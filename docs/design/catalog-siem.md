# Catalog — SIEM surface details

Per-surface detail for the SIEM (Chronicle) plane. The status matrix and the
compact tables are the spine in [catalog.md](catalog.md); this page expands each
row into a `##### <function>` entry. Keep the two in lock-step.

## Control plane — config as code (`pull` → `git diff` → `push`)

### `rules`

YARA-L source + deployment state machine (two resources, not a single canonical body).

CLI verbs:

- `push rules-create` — initial `--enabled`, `--alerting`, `--run-frequency`; the summary and per-rule line flag a rule that was created but landed `enabled=false` (a platform complexity/volume guard re-issues at HOURLY, but a very high-volume rule can still land disabled), so it is not mistaken for live (`rules promote` carries the same note)
- `rules-update` — etag-guarded text update
- `rules-deploy` — reconcile enabled/alerting/frequency; archived rules reported non-deployable; the apply PATCH is field-masked to only the changed fields (an alerting-only flip sends `alerting` alone, no longer 409s on unchanged `enabled`); summary reports `deployed` / `already in desired state` / `failed` truthfully — verified: an alerting-only flip reports `1 deployed, 0 failed` and a re-deploy is an in-sync no-op
- `rules-disable`
- `rules detections/errors/alerts <rule>` — rule id, display name, or slug resolved against the live rule list; clean `no rule matches` error on a miss instead of the API's opaque 400
- `rules retrohunt list/get/create`
- `rules errors` — decodes both string and structured execution-error payloads

Rule-tuning reads (Wave 54, verified):

- `rules trends` — per-rule day-bucketed detection counts + last detection, noisiest first; no `--rule` sweeps every rule; `legacyGetRulesTrends`; bucket window must be bucket-ALIGNED or the API 400s generically — the SDK floors/ceils
- `rules counts` — rule + quota stats (`legacyGetRuleCounts`)
- `rules events <rule> <detection-id>` — the UDM evidence behind one detection (`legacySearchRuleDetectionEvents`); summary per event variable, full payloads with `--json`
- `SearchRuleDetectionCountBuckets` — SDK-only

Batch update: `rules:modifyRules` SDK (`ModifyRules`, per-index failure map) — built, exercised by the gated lifecycle smoke; the reconcile sweep stays per-rule PATCH until the batch path passes an approved smoke.

Also: `ArchiveRule`. **`rules test <file.yaral>` (W95)** wires `RunTestRule` — dry-run a YARA-L rule against the last `--hours` of data and report the detections it would produce, WITHOUT creating the rule (beyond `validate`'s compile-check); compile errors surface, nothing is stored. Verified.

#### entities graph / data-access (W95)

**`entities graph <detection-id>`** seeds the findings-graph pivot (`InitializeFindingsGraph`): root node + connected entities/edges over `--hours`; `entities graph explore --param k=v` expands a node (`ExploreFindingsGraphNode`). Read-only, JSON; verified (rootNode + adjacent nodes for a real detection). **`data-access labels|scopes`** list/get/create/delete wire the `chronicle/rbac.go` CRUD — the data-access RBAC that was console-only; create/delete guarded, reads verified.

Read verified; lifecycle write smoke `TestLiveRulesLifecycleWriteSmoke` (create→update→deploy→archive→modifyRules→delete, self-cleaning).

`rules promote <file.yaral>` (Wave 78): validate + create + deploy a brand-new rule in one guarded step (replaces `rules-create` then `rules-deploy`); writes the companion `.yaml`, refuses a file that already has one, shares the LIVE→HOURLY multi-event fallback. Verified by the gated lifecycle smoke (promote an inert disabled rule → deploy → delete, self-cleaning).

**Rules dev-loop + workspace parity (Wave 115, v0.6.1):**

- **`rules get <rule>`** — the current rule in one shot: a state header (running/enabled, alerting, severity, compile + execution state, run frequency, author, MITRE, current revision) followed by the YARA-L. `--text` prints just the YARA-L (raw, pipes into `rules promote` / a diff); `--json` emits the full rule + deployment. Wraps `GetRule` (the latest version) so an operator reads the live rule without addressing a revision.
- **`rules test` now STREAMS** — it decodes the `legacy:legacyRunTestRule` chunk stream incrementally (progress percent + detections + compile/runtime errors), printing detections and scan progress on stderr as they arrive rather than buffering the whole window. `--no-stream` keeps the buffered path; `--from`/`--to` set an explicit window alongside `--hours`. The JSON shape (`detection_count` / `detections` / `compilation_errors` / `runtime_errors`) is identical on both paths, and a compile or runtime error fails the run (non-zero exit) even under `--json`. The streaming primitive (`chronicle.streamArray`) decodes the JSON-array body element-by-element and does not retry (non-idempotent POST).
- **`mitre`** — MITRE ATT&CK coverage as data: a per-technique aggregation over custom rules (`metadata.mitre_tactic` / `mitre_technique`) + curated rules' typed tactics/techniques, with the rule count, involved tactics, and rule ids per technique, and an `UNMAPPED` bucket for rules with no MITRE meta. `--type custom|curated|all`, `--enabled`/`--alerting` (deployment-authoritative, like `rules health`), `--format table|json|csv`, `--out`.
- **`rules versions diff <rule> <a> <b>`** / **`restore <rule> <version>`** — `diff` shows a line-by-line diff of two revisions' YARA-L (each ref a 1-based index from `rules versions` or a `v_…` token; `GetRuleRevision` addresses `rules/{id}@{version}`); the guarded `restore` re-applies a prior revision's text as a NEW revision (etag round-tripped, refuses an empty target). The `versions` parent now resolves an id/name/slug like the sibling verbs.
- **`rules duplicate <rule> [--name]`** — guarded clone of a rule's YARA-L under a new name token, created disabled (no deployment); refuses a name collision.
- **`rules health`** — per-rule health roll-up classified failing/erroring/silent/healthy (worst-first): compile state + deployment execution state + detection volume/last-detection over `--hours`. Composes `ListRules` + `ListRuleDeployments` + `GetRulesTrends`, no new endpoint; `--only`, `--format`, `--out`.
- **Model:** the FULL-view `Rule` now carries `author` / `compilationState` / `runFrequency` / `liveModeEnabled` / `alertingEnabled` / `metadata{…}` and `MitreTactics()` / `MitreTechniques()` accessors (split on commas/semicolons only, so multi-word names stay intact). Offline-tested.

### `reference_lists`

Typed `.txt`+`.yaml`; NoDelete; product-neutral engine.

Resource-name normalization: create echoes the project NUMBER while list echoes the project ID — both rewritten to the id form so reconcile identity (keyed on the name) stays stable.

Guarded `lists empty <name>` clears entries for no-delete neutralization without printing values (the reconcile target stays `pull`/`push reference_lists`).

Write smoke `TestLiveReconcileReferenceListWriteSmoke` reuses one fixed inert list (no delete API) — fresh create-or-reuse + one update each run (rerunnable, no accumulation).

An empty list canonicalizes to `[]` on both sides (entries normalized non-nil), so `drift` no longer phantom-reports it as changed after a clean pull.

### `data_tables`

`.csv`+`.yaml` on the engine; `push data_tables` (create/update).

Columns immutable after create; rows are wholesale destroy-and-replace (`ReplaceDataTableRows`).

Not prune-eligible (whole-table delete is high-blast).

Write smoke `TestLiveReconcileDataTableWriteSmoke` (create→update desc→replace rows→delete).

### `feeds`

`.yaml` on the engine; `push feeds`.

Secrets redacted on pull, overlaid on update (real secret preserved; create refuses a masked body); new credentials can be sourced at apply time with `secret_ref: env:VAR` or `secret_ref: secretmanager:projects/<project>/secrets/<secret>/versions/latest` (never in YAML).

`details` replaced wholesale on PATCH. `assetNamespace` (read) vs `namespace` (write) reconciled — the API uses `assetNamespace`; short `logType` expanded to the full resource name on write.

Feed state is a runtime toggle, out of canonical. Not prune-eligible (delete stops ingestion) — deletion is the explicit, guarded `ingest feeds delete <id>` imperative (the reconcile target stays `pull`/`push feeds`).

Write smoke `TestLiveReconcileFeedWriteSmoke`; GCS V2 (`gcsV2Settings`, STS-backed) validated; `FetchFeedServiceAccount` for the STS SA grant; templates live in `examples/feed-templates/`.

### `parsers`

`.conf`+`.yaml` on the engine; `push parsers`.

Versioned/immutable — no server-side update: an edit is create-new-version + activate (parser id volatile, written back on refresh); old version left inactive (rollback available).

Not prune-eligible.

Write smoke `TestLiveReconcileParserWriteSmoke` runs `RunParser` (pure inert validation) then creates a new INACTIVE version, asserts it never goes ACTIVE (live ingestion untouched), deletes it.

`RunParser` response shape: `parsedEvents` is `{events:[…]}`.

Parser-dev loop (read):

- `ingest parsers sample-logs <type>` — lists recent raw logs directly (`logTypes/<type>/logs`, `data` base64-decoded — the simplest raw-log path, no search)
- `ingest parsers run` — submit CBN for test
- `push parsers` — submit for activation
- `ingest parsers validate <type>` — surfaces the submitted parser's validation-report parsing errors (`{parser}.validationReport` + `.../parsingErrors`: the per-log `error` message + failing log) — the detail behind a `push`/`activate` `FAILED_PRECONDITION` that otherwise gives no reason

Read-verified. SDK files: `chronicle/logs.go`, `chronicle/parsers.go`.

### `dashboards`

Native dashboards (CUSTOM only; CURATED read-only/unmanaged); `pull`/`push dashboards`.

One `<slug>.json` (config + `_server` id). `definition.charts[]` is chart-reference-only by API design — the YARA-L query lives in separate `dashboardCharts`→`dashboardQueries` resources, not the dashboard body.

Pull modes (Wave 72):

- Default — keeps charts as references (layout + filters + `_server.chart`; cheap, deterministic drift)
- `pull dashboards --with-charts` — derefs each chart inline (`+query/interval/visualization`; the chart bodies are read in one `dashboardCharts:batchGet` per dashboard via `ChartsByID`, falling back to per-chart `GetChart` for a dashboard with a dangling chart; the query is still a `GetQuery` per chart — no batch endpoint; 404/dangling charts degrade gracefully to references)

`push`/`drift` detect an inline mirror and deref the live side to match; `push` of an inline mirror reconciles new charts (`:addChart`) and changed queries/title/viz (`:editChart`, etag-guarded).

Chart layout/filters/reorder reconcile (Wave 79) via a `definition.charts` PATCH — but ONLY when the desired and live chart sets are identical (the PATCH replaces the array wholesale, so a differing set could drop a chart); a membership change defers the layout PATCH to the next push.

Chart removal stays reported, not applied (no `--prune` gate for sub-resources; deleting a chart absent from a stale mirror would destroy an out-of-band edit) — remove with `dashboards remove-chart`; datasource edits and visualization clears likewise stay reported.

Dry-run is schema-checked (Wave 79): an optional `reconcile.Surface.Validate` hook rejects an API-invalid body (missing displayName, empty new chart, bad tileType, non-object chartLayout) in the preview and aborts a real apply.

`pull --with-charts` reports the count of charts that degraded to a reference (404/429) in the pull summary.

Per-chart `_server` stripped from the diff basis. `access` immutable after create. The list view returns a stub definition (no charts), so each CUSTOM dashboard is fetched in full view. Server-managed actor ids (`createUserId`/`updateUserId`) are stripped globally by the engine (`reconcile.actorKeys`, like the time fields) so no surface leaks a tenant user id; `dashboardUserData` (per-viewer state) dropped by the dashboard extraStrip; root `name` stripped (identity in ServerID).

Write smoke: create→update→delete, closure-direct to dodge full-view rate-limiting.

Chart-query authoring (Wave 70 SDK, Wave 71 CLI, verified): the SDK `AddChart`/`EditChart`/`RemoveChart`/`GetChart`/`GetQuery` author a chart's YARA-L via `:addChart`/`:editChart` (`dashboardQuery{query,input}`, etag-guarded); proven by `TestLiveDashboardChartAuthoringWriteSmoke` (query round-trips; dashboard definition stays reference-only). Surfaced as guarded CLI verbs `dashboards add-chart` / `edit-chart` / `remove-chart` + read-only `dashboards charts` (derefs each chart→query), verified by `TestLiveDashboardsChartCLISmoke`.

Execute and verify (Wave 82, verified): `dashboards run-chart <id> --chart-id <c>` (alias `values`) derefs the chart and executes its query (`ExecuteQuery` / `dashboardQueries:execute`), printing the computed rows/series (`--json`, `--clear-cache`, `--filter`). `dashboards verify <id>` executes every chart and flags 0-row/errored charts (a headless/CI health check; exits 2 on any bad chart).

Execute response shape: `results[]` is column-major — a list of `{column, values[]}` — so the row count is the longest `values` column (NOT the number of columns), and an all-empty-values chart is flagged EMPTY; an unfamiliar shape is never mis-flagged.

Authoring ergonomics (Wave 83, verified):

- `add-chart`/`edit-chart --chart-type bar|line|pie|table --x --y [--series-by]` — generate the echarts `visualization` and validate encode vars against the query's columns (both `outcome:` `$vars` AND bare `match:` field references such as `target.hostname`; a typo fails clean, a valid field is accepted)
- `edit-chart` edits visualization/layout in place (visualization via `:editChart`; layout via the `definition.charts` PATCH, since `chart_layout` is not an `:editChart` field — preserving every other chart)
- `add-chart --if-absent` + batch `add-charts --file <charts.json>` (validated up front, idempotent skip-by-title, `--pace`d under the chart quota) make a whole-dashboard build rerunnable
- `add-chart`/`edit-chart` warn at author time when a `match:`/`outcome:` variable name collides with a reserved YARA-L keyword (e.g. `$rule`, `$events`) — these compile but 400 at execute time, rendering a blank chart; the warning names the offenders and suggests a rename. The reserved set is the YARA-L keyword reference (sections, modifiers, operators, size specifiers)

Verified end to end by `TestLiveDashboardsAuthoringSmoke` (generated viz stored as a BAR series; run-chart/verify; in-place type/layout edit keeps the chart id; batch idempotent). Lossless deref-on-pull round-trip is a follow-up.

Copy and delete (verified). `dashboards duplicate` defaults to the server **`:duplicate`** verb (`DuplicateDashboard`) — the same path the web console's Duplicate action takes — which mints the copy its **own independent charts and queries** in a single call (the copy shares no chart or query id with the source). `--deep-copy` selects the client-side fallback (`DeepCopyDashboard`): create a new dashboard with the source's filters, then recreate every chart fresh (source charts read in ONE `dashboardCharts:batchGet`; each query read via `GetQuery` and replayed inline through `AddChart`) — also fully independent, for when a server-side copy is unavailable. `dashboards delete` removes a whole dashboard (clean deletes succeed); when the backend returns a 500 it is diagnosed: a **corrupt** dashboard whose `definition.charts` hold dangling/non-owned references (charts owned by another dashboard or already gone) cannot be deleted by the API **or the web console**, the corrupt state can't be repaired first (`:removeChart` 404s; rewriting `definition.charts` 400s), and deleting the chart owner does not unstick the 500 — so it can only be removed by the platform (raise with Google support). Such corrupt dashboards are a legacy artifact, **not** something the current `:duplicate` verb produces.

Portability (verified). `dashboards export <id>` writes a self-contained JSON document — the dashboard plus its charts and queries (the `nativeDashboards:export` `inlineDestination.dashboards[0]` object) — and `dashboards import <file>` re-creates it in ONE `nativeDashboards:import` call (the server mints fresh ids, so an import shares no id with the source). A faster build path than `duplicate` + per-chart `add-chart`, and the way to move a dashboard between instances.

### `curated` / `curated_rules`

Google-managed (no CUD): `pull curated` writes `curated/deployments.yaml`; `push curated` diffs it against live and calls `BatchUpdateCuratedRuleSetDeployments` for changed enabled/alerting tuples.

**Curated browsing (v0.6.1):** natural drill-down workflow: `curated categories` (12-row overview with set/enabled counts) → `curated rule-sets` (default enabled only; `--all` for catalog; `--search`/`--category` accept display name or UUID; deployment state inline per precision) → `curated rules --set <id>` (individual rules in one set; `--all` opt-in for full dump; ~80% of sets are opaque) → `curated rule <id>` (detail with resolved set/category display names). `curated search <query>` is the unified search across both rule sets and individual rules (`--installed`/`--tactic`/`--severity`). Guarded `curated set` remains the one-off toggle. Enrichment uses 3 API calls (wildcard rule-set list + categories + deployments). The old `curated list` is removed (merged into `rule-sets`). The `CuratedRule` model carries `type`, `precision`, MITRE `tactics`/`techniques`, `description`, and the parent **`curatedRuleSet`** (the rule→set membership). Curated YARA-L source is **not** API-exposed (Google-managed).

Curated tuning reads (Wave 54, verified):

- `curated detections <ur_id>` — `legacySearchCuratedDetections` (the curated twin of `rules detections`, which serves user rules only)
- `curated trends --rule ur_a,… | --all` — `legacyGetCuratedRulesTrends`, day-bucketed counts + last detection; `--all` sweeps every curated rule in chunked requests
- `curated events <detection-id>` — `legacyGetEventForDetection` (event + rationale behind one curated detection; answers 200 but can be empty for some detection types, e.g. GCTI_FINDING)

Batch update verified by `TestLiveCuratedBatchToggleWriteSmoke` (self-restoring enable→verify→restore, alerting off).

v1alpha is the only version that answers for curated rules — v1/v1beta 404.

`curated set` blast-radius preview (Wave 78): before the guard, a best-effort read shows the addressed deployment's current → requested enabled/alerting state and the reminder that a deployment is set × precision. A set's rules are now enumerable with `curated rules --set <id>`, and per-set detection counts are available via `countAllCuratedRuleSetDetections` (a future inline blast-radius count); `curated trends` / `curated detections` show detection volume today.

### `rule_exclusions`

Findings refinements (display_name + type + UDM query + deployment enabled/archived); `pull`/`push rule_exclusions` round-trip both core config and deployment state.

Create + Update (PATCH/updateMask); deployment updates use the separate deployment PATCH. NoDelete (drift reported, never pruned); NoEtag.

Guarded `exclusions deploy <id> --enable|--disable|--archive` handles one-off toggles (the reconcile target stays `pull`/`push rule_exclusions`).

Read + write verified (create→update→archive). The API has no hard delete — archive is the teardown.

### `forwarders`

`.yaml` on the engine; full `pull`/`push`/`drift forwarders`.

Diff basis is `display_name` + the freeform `config` block (uploadCompression, metadata, serverSettings, …); runtime `state` and server-stamped times stripped from the canonical. Config replaced wholesale on PATCH so Update overlays local edits onto the live body.

NoEtag; prune-eligible (clean delete-by-id). Collectors are a separate nested resource.

SDK pinned v1beta (v1 404s).

Write smoke `TestLiveReconcileForwarderWriteSmoke` (inert throwaway, serverSettings disabled, create→update config→delete, self-cleaning).

### `metric_definitions`

Custom SOC metrics (id = display name; `text_definition` is YARA-L 2.0); `pull`/`push metric_definitions`.

Additive (create + state-only patch); `textDefinition` immutable — a text edit is refused; change requires a new id. No delete API → NoDelete.

One `<slug>.yaml` (display_name, name, state, text_definition). Built + offline-tested; the read returns 403 where the feature is not enabled (Pre-GA), so the surface is not yet verified.

SDK file: `chronicle/metrics.go`.

### `scheduled_reports`

Scheduled dashboard reports (`dashboardScheduledReports`): recurring PDF/CSV/PNG delivery of a native dashboard on a cron; full CRUD with etag; `pull`/`push scheduled_reports`.

One `<slug>.json` (config + `_server` id/etag); the embedded `dashboard` is reduced to its `{name}` reference (the dashboard is managed separately). Prune-eligible (clean delete-by-id).

Imperative `trigger`/`duplicate`/`fetchHistory` in the SDK.

Reads verified (list 200); the create-report backend currently 500s "failed to fetch native dashboard details" (a server-side issue — the `{name}` ref shape is accepted and parsed), so the write-smoke (`TestLiveReconcileScheduledReportWriteSmoke`) skips on that 500.

SDK file: `chronicle/scheduled_reports.go`.

### `datataps`

Stream UDM events to a Cloud Pub/Sub topic (`dataTaps`): `pull`/`push datataps`.

One `<slug>.yaml` (display_name, name, filter ALL/ALERT/LABELED, serialization_format JSON_OBJECT/MARSHALLED_PROTO defaulted, topic). Prune-eligible; NoEtag.

Write verified (`TestLiveReconcileDataTapWriteSmoke`, create→update→delete on an inert tap pointed at a nonexistent topic).

PATCH is 501 UNIMPLEMENTED on the backend, so an update is done as delete-old + create-new (the id is server-assigned and changes); `UpdateDataTap` is kept for when PATCH lands.

Supersedes the legacy Backstory `dataTaps`. Prereq for a live tap: grant Pub/Sub Publisher to `publisher@chronicle-data-tap.iam.gserviceaccount.com`.

SDK file: `chronicle/datataps.go`.

### `error_notifications`

Ingestion-health alerts (`errorNotificationConfigs`): zero-ingest / size-threshold / normalization-delay → Cloud Monitoring channels; `pull`/`push error_notifications`.

One `<slug>.json` (displayName, enabled, notificationChannels + one oneof notification_type block kept raw) + `_server` id. Full CRUD, prune-eligible, NoEtag; updateMask derived from present keys (the oneof masks as `notification_type`).

Built + offline-tested; feature-gated 403 unless the feature is enabled, so not verified.

SDK file: `chronicle/error_notifications.go`.

### `enrichment_controls`

Turn off a UDM enrichment per log type / enrichment type (`enrichmentControls`).

SDK `ListEnrichmentControls`/`Get`/`Create`/`Disable`/`Delete` in `chronicle/enrichment_controls.go`.

Imperative, not reconcile — there is no patch; a create for an existing control appends a time-ranged record and `:disable` closes the latest record, so config-as-code round-tripping does not fit.

Built + read-attempted; feature-gated 403 unless the feature is enabled.

### `federation_groups` · `tenants` (MSSP)

Multi-tenant federation. `federationGroups` group subtenant instances (`pull`/`push federation_groups`; typed reconcile, prune-eligible, NoEtag); `tenants` (partner-only enumeration) + `multitenantDirectory` (super/subtenants of this deployment) are SDK reads in `chronicle/federation.go`.

MSSP-only — on a single tenant `federationGroups`/`tenants` are 403 (feature/partner-gated); `multitenantDirectory` read-validated (returns this deployment).

Built + offline-tested.

### schema discovery — `feedSourceTypeSchemas` · `logTypeSchemas` · `logTypeSetting` · `logTypes.get`

SDK file: `chronicle/schemas.go`.

Methods: `ListFeedSourceTypeSchemas` (available feed source types), `ListLogTypeSchemas(sourceType)` (accepted log types + required detail fields per source type), `GetLogTypeSetting` (per-log-type ingestion config), `GetLogType` (single log type — a documented v1alpha method).

The reference for validating feed YAML before a deploy. Read-only (upstream-defined). Rides the feeds family — project ID form, v1alpha default. Read-verified (`TestLiveSchemaDiscoveryRead`).

Per-log-type GET (`logTypes.get`) is wired but is a documented method some instances don't enable — it can 404 "Method not found" across all versions and both hosts; in that case enumerate with `ListLogTypes`. Follow-up: wire into feed validation.

### governance — `riskConfig` · `dataAccessLabels` / `Scopes`

SDK-only (no CLI yet). SDK file: `chronicle/rbac.go` (`rbacAPIVersion`; all three versions answer → pinned v1).

Write-validated (Wave 10):

- `dataAccessLabels` CRUD — `TestLiveDataAccessLabelWriteSmoke`
- `dataAccessScopes` CRUD — `TestLiveDataAccessScopeWriteSmoke` (a throwaway unassigned scope allowing a throwaway label)
- `riskConfig` GET + idempotent `UpdateRiskConfig` — `TestLiveRiskConfigWriteSmoke` (same-value update), self-cleaning

All smokes self-cleaning on inert throwaways.

Create-body shapes: a label needs a `udmQuery`; a scope sets `allowAll` + `allowed/deniedDataAccessLabels:[{dataAccessLabel:<labelId>}]`.

The surface is quirky: create can return an error yet still persist; create→list lags; deleted ids tombstone; body `displayName` is ignored. It is therefore operated imperatively (unique ids + delete-by-exact-id), not via the reconcile engine (list lag breaks diffing).

## Operational plane — query → act (live data)

### events (UDM) — `search`

Immutable telemetry — **read-only, never mutated**. The deterministic `search` suite runs UDM predicates and aggregations against the events store; NL→UDM translation lives in the `gemini` group (below).

Verbs:

- `search udm '<filter>'` — event search over a window (`--hours`/`--from`/`--to`, `--limit`)
- `search stats '<aggregation YARA-L>'` — run a `match:`/`outcome:` aggregation and print the computed columns/rows (W81)
- `search raw '<regex>'` / `search udm --raw` — raw-log retrieval (see *raw logs* below)
- `search event <id>` — fetch a single UDM event by id
- `search validate '<filter>'` — compile-check a UDM predicate without running it
- `search run --file <path>|-` — run a UDM predicate from a file or stdin (blank and `#`-comment lines ignored — `examples/queries/*.udm` run directly; W75)
- `search saved [<name>]` — run a tracked local `saved_queries/` pack query by name, or list (path-sanitized; W75) — distinct from the server-side `search saved` suite below
- `search export` — stream a full result set to a file

All reuse the shared `runUDMQuery` core, so window/`--limit` semantics match across verbs.

**Agent-first output contract (v0.6.0):** every search verb takes `--format jsonl|json|csv|table` (default human table), `--fields <dotted.udm.path,…>` to project specific UDM fields, `--out <file>` to stream results to disk, and `--all` to fetch the complete result set and report the total match count (rather than just the first page). `--json` remains the back-compat alias for `--format json`. SDK: `chronicle/search.go`, `search_types.go`, `udm_search_view.go`, `udm_search_csv.go`, `stream_search.go`.

**`search stats` (W81; aggregation path re-routed in W101):** a plain event search (`search udm`) 400s on a `match:`/`outcome:` aggregation; `search stats` runs the aggregation and prints the computed columns and rows — the CLI way to validate the exact YARA-L a dashboard chart uses before authoring. The `match:` section takes a field reference (`target.hostname`), not `$var = field`. Flags parse in any position (interspersed). The aggregation is executed over the **POST `dashboardQueries:execute`** path (`chronicle.ExecuteQuery`) — the same execution `dashboards run-chart` uses; the GET `:udmSearch` stats path (`chronicle.GetStats`) returns `400 INVALID_ARGUMENT` for `match:`/`outcome:` aggregations and is not used for this verb.

### Saved & shared searches — `search saved` (v0.6.0)

Server-side saved searches (the SecOps "saved searches" the console persists), distinct from the local `saved_queries/` pack that `search run`/`search saved <name>` read. SDK: `chronicle/saved_searches.go`.

- `search saved list` / `get <id|name>` / `run <id|name>` — read + execute (read)
- `search saved save <name> '<filter>'` — persist a search server-side; the `searchQueryId` is client-generated (guarded)
- `search saved share <id>` / `unshare <id>` — flip `SharingMode` between `MODE_PRIVATE` and `MODE_SHARED_WITH_CUSTOMER` (visible author-only vs whole instance) via a `metadata.sharingMode` PATCH (guarded)
- `search saved delete <id>` — remove a saved search (guarded)

`save`/`share`/`unshare`/`delete` are guarded (dry-run by default).

### raw logs

Recent **full** raw (ingested) log lines for parser development — two complementary paths, both fetching the complete bytes by raw-log id via `legacyFindRawLogs?ids=` (GET, batches of 25; `logBytes` base64-decoded), one line per log → pipe into `ingest parsers run --logs -`.

**(1) `search udm '<filter>' --raw`** — scopes by UDM metadata (`metadata.log_type`), lifting each event's `udm.metadata.id`; precise, but requires a UDM event. A log type whose parser is missing or broken normalises to **GENERIC_EVENT** (still `parsed = true`), so `metadata.log_type = "<TYPE>" AND metadata.event_type = "GENERIC_EVENT"` returns exactly the logs a parser fix targets; `--limit` defaults to 100.

**(2) `search raw '<regex>'`** — content-based `:searchRawLogs` (`raw = /<regex>/`), matching the raw bytes; reaches even logs with **no parser at all** (no UDM event). `--unparsed` adds `parsed = false`.

Note: the `:searchRawLogs` `logTypes` body filter is **ignored server-side** (confirmed across code/displayName/resource-name forms) — log-type scoping comes from the UDM path; content scoping from the regex. Read-verified (`TestLiveFetchRawLogLines`). SDK: `chronicle/log_search.go`.

### alerts

`alerts list` takes a snapshot over a time window (`legacyFetchAlertsView`, streams a JSON-array of progressive fragments). `alerts get <id>` uses `legacyGetAlert` (response wrapped under `alert`). **Read-verified** (`chronicle/alert.go`; fixed the array-stream decode, the `createdTime`/`detectionTime` keys, and `severityDisplay` being a string).

**Act is CLI-wired (W52):** guarded `alerts update <id>…` sets feedback (status/verdict/priority/reason/reputation/scores/comment/root-cause) over `UpdateAlert`/`BulkUpdateAlerts`; multiple ids fan out. Enum flags accept short forms (`closed`, `false-positive`, `high`) normalised to wire tokens and validated client-side (`AlertUpdate.Validate`) before the guard — dry-run validated; write smoke gated.

**Alert→case bridge (W52):** `alerts get` resolves the alert's SIEM case UUID to the SOAR integer id (fail-soft). `cases soar-id <uuid…>` is the bulk bridge (`legacyBatchGetCases`) — both read-verified. Operators also read alerts as a **field of the case** via the reliable SOAR lane (`GetCaseFullDetails.alerts`).

**Bulk disposition (W76):** `alerts update --where <snapshot-filter>` resolves a known FP burst to ids (over `--hours`, capped by `--limit`). `--stdin-ids` reads ids on stdin; sources merge and de-duplicate, and the resolved set prints before the guard. A `--where` matching more than `--limit` is a hard error (refuses a silent partial update) — **verified** (dry-run resolves the id set; a too-low `--limit` refuses; the bulk write rides the W52-validated `BulkUpdateAlerts`). `alerts list` surfaces a completeness signal: the pre-filter baseline count and a stderr truncation warning (the alerts snapshot has no server cursor).

### entities

`entities summarize` returns alerts/related/prevalence — enrichment, read-only. Counter fields (`alertCounts.count`, timeline buckets, widget totals, prevalence counts) decode via a tolerant `flexInt` — the API renders int64 counters as JSON strings (proto3 JSON). `entities risk-scores` reads behavioural risk; `entities graph <detection-id>` / `entities graph explore` seed and expand the findings-graph pivot (see *entities graph / data-access* above).

### watchlists — `lists watchlists`

SIEM entity watchlists, surfaced under the `lists` group. `lists watchlists list`/`get <id>` — read-validated. Pinned **v1** (`watchlistsAPIVersion`; all three API versions answer, so v1 is selected).

### analytics & AI reads — investigations, entityRiskScores, bigQueryExport, coverageDetails

SDK: `chronicle/analytics.go` (Wave 17).

Gemini **TIN** investigations (250) and steps are read-validated (list/get/trigger in `investigations.go`). `entityRiskScores:query` (301) returns behavioural risk 0–1000 on v1alpha default. `coverageDetails` returns MITRE coverage (5). `bigQueryExport` get is included. Both `coverageDetails` and `bigQueryExport` are pinned **v1**. `investigationComments` is wired and returns clean typed errors when not provisioned or implemented (501/400, Pre-GA/Enterprise+). Test: `TestLiveAnalyticsRead`. Writes (`:provision`/`update`) are gated, not wired.

### alert AI investigation — alerts investigate

The per-alert Gemini TIN triage flow (W57). `alerts investigate <alert-id>` triggers an investigation, polls to `STATUS_COMPLETED_*`, and prints verdict/confidence/summary/next steps. `--json` adds the embedded investigation steps with the agent's actual UDM queries. `--latest` is the read-only variant (the UI's `alert_id='…' AND latest_in_alert=true` filter, no generation).

The typed `Investigation` carries status/verdict/confidence/summary/nextSteps/notebook/triggerType. `GetNotebook` reads the agent's working document (`notebooks/<id>` — a resource absent from the public REST index). **Verified end-to-end** (trigger + filtered list + notebook in `TestLiveInvestigationTriggerRead`; `--latest` render against a completed investigation).

### Gemini — `gemini ask` / `generate` / `search`

The AI search group. SDK: `chronicle/gemini.go` (assistant) + `chronicle/nl_search.go` (NL→UDM).

- `gemini ask '<question>'` asks SecOps Gemini (YARA-L authoring help, UDM field questions, environment-grounded answers) over the `chronicle/gemini.go` conversation flow. Verified (W56). Replies carry HTML blocks, rendered as plain prose on the human path (`--json` for the full block structure).
- `gemini generate '<natural language>'` translates NL→UDM and prints the query **without running it** (`TranslateNLToUDM`); the model also returns an inferred time range (`TranslateNLToUDMWithTimeRange`).
- `gemini search '<natural language>'` translates NL→UDM **and runs it** — honoring the model's suggested time window — then renders results through the same `--format`/`--fields`/`--out`/`--all` output contract as `search udm`.

`--opt-in` runs the one-time account opt-in (required before any Gemini call). Under hard read-only mode the artifact-creating generations refuse cleanly.

### findings graph — findingsGraph

Graph-pivot investigation (W56). `InitializeFindingsGraph` seeds a graph from a **detection id** over a time range; `ExploreFindingsGraphNode` expands nodes (node ids are tied to the initialising range). **Read-verified** (`TestLiveWave56Read` — a real detection seed returned root and graph). SDK-only (`chronicle/findings_graph.go`) — the structured "what is connected to this detection" primitive.

### alert enrichment — alerts enrich

`alerts enrich <id>` (read-only, verified) fetches the full per-alert detection collection the console renders when an analyst opens an alert: the rule detection(s), every mapped UDM event, the involved entities/indicators (hosts, users, process path+sha256, domains), the alert's MITRE tags, its SOAR case linkage, and the AI triage verdict when an agent has run. It reads `legacy:legacyBatchGetCollections?collectionIds=<id>` (`chronicle/legacy.go` `BatchGetCollections`, chronicle host / ADC) — the surface the web UI uses. The AI agent's investigation detail is `alerts investigate <id> --latest`.

The earlier `enrichmentAgent:*` path (W56) is a dead end: `fetchAlertData`/`fetchActions`/`executeActions` return **500 INTERNAL** for every variant (across versions and auth forms — AppKey-in-query is a SOAR-host concern, never chronicle) and are **not used by the console**. The pre-case "run an integration action against an alert's entities" verbs that rode it (`actions`/`run-actions`) are therefore withheld rather than shipped always-failing; the SDK methods stay importable (`chronicle/enrichment_agent.go`) for if/when the real surface is captured. The in-case equivalent — `cases run-action` — works today.

### watchlist membership — `lists watchlists add-entity`

`lists watchlists add-entity <id> (--ip|--mac|--hostname|--user|--email)` puts an entity on a watchlist (`entities:add`, exactly-one-selector contract) — a containment/tracking response action; membership feeds the risk-score multiplier. The request's `entity` is the UDM Entity **envelope** (`{entity: <Noun>}` — the selector sits on the inner Noun, one level below; a flat noun is rejected 400). `RemoveWatchlistEntity` removes by the entity resource name the add response returns (`entities.remove`; CLI `lists watchlists remove-entity`); `BatchRemoveWatchlistEntities` is SDK-raw. Watchlist CRUD itself (`lists watchlists create`/`delete`) is **write-verified**. The membership ops can answer 501 UNIMPLEMENTED per instance — the gated smoke (`TestLiveWatchlistEntityWriteSmoke`, self-contained on a throwaway watchlist) skips cleanly there.

---
