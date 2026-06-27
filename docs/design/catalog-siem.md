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

Also: `ArchiveRule`. **`rules test <file.yaral>` (W95)** wires `RunTestRule` — dry-run a YARA-L rule against the last `--hours` of data and report the detections it would produce, WITHOUT creating the rule (beyond `validate`'s compile-check); compile errors surface, nothing is stored. Live-validated (5 detections over a real window).

#### entities graph / data-access (W95)

**`entities graph <detection-id>`** seeds the findings-graph pivot (`InitializeFindingsGraph`): root node + connected entities/edges over `--hours`; `entities graph explore --param k=v` expands a node (`ExploreFindingsGraphNode`). Read-only, JSON; verified (rootNode + adjacent nodes for a real detection). **`data-access labels|scopes`** list/get/create/delete wire the `chronicle/rbac.go` CRUD — the data-access RBAC that was console-only; create/delete guarded, reads verified.

Read verified; lifecycle write smoke `TestLiveRulesLifecycleWriteSmoke` (create→update→deploy→archive→modifyRules→delete, self-cleaning).

`rules promote <file.yaral>` (Wave 78): validate + create + deploy a brand-new rule in one guarded step (replaces `rules-create` then `rules-deploy`); writes the companion `.yaml`, refuses a file that already has one, shares the LIVE→HOURLY multi-event fallback. Live-validated (promote an inert disabled rule → deploy → delete, self-cleaning).

### `reference_lists`

Typed `.txt`+`.yaml`; NoDelete; product-neutral engine.

Resource-name normalization: create echoes the project NUMBER while list echoes the project ID — both rewritten to the id form so reconcile identity (keyed on the name) stays stable.

Guarded `reference_lists empty <name>` clears entries for no-delete neutralization without printing values.

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

Feed state is a runtime toggle, out of canonical. Not prune-eligible (delete stops ingestion) — deletion is the explicit, guarded `feeds delete <id>` imperative.

Write smoke `TestLiveReconcileFeedWriteSmoke`; GCS V2 (`gcsV2Settings`, STS-backed) validated; `FetchFeedServiceAccount` for the STS SA grant; templates live in `examples/feed-templates/`.

### `parsers`

`.conf`+`.yaml` on the engine; `push parsers`.

Versioned/immutable — no server-side update: an edit is create-new-version + activate (parser id volatile, written back on refresh); old version left inactive (rollback available).

Not prune-eligible.

Write smoke `TestLiveReconcileParserWriteSmoke` runs `RunParser` (pure inert validation) then creates a new INACTIVE version, asserts it never goes ACTIVE (live ingestion untouched), deletes it.

`RunParser` response shape: `parsedEvents` is `{events:[…]}`.

Parser-dev loop (read):

- `parsers sample-logs <type>` — lists recent raw logs directly (`logTypes/<type>/logs`, `data` base64-decoded — the simplest raw-log path, no search)
- `parsers run` — submit CBN for test
- `push parsers` — submit for activation
- `parsers validate <type>` — surfaces the submitted parser's validation-report parsing errors (`{parser}.validationReport` + `.../parsingErrors`: the per-log `error` message + failing log) — the detail behind a `push`/`activate` `FAILED_PRECONDITION` that otherwise gives no reason

Live read-validated against KONG_GATEWAY. SDK files: `chronicle/logs.go`, `chronicle/parsers.go`.

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

Live-validated end to end by `TestLiveDashboardsAuthoringSmoke` (generated viz stored as a BAR series; run-chart/verify; in-place type/layout edit keeps the chart id; batch idempotent). Lossless deref-on-pull round-trip is a follow-up.

Copy and delete (verified). `dashboards duplicate` defaults to the server **`:duplicate`** verb (`DuplicateDashboard`) — the same path the web console's Duplicate action takes — which mints the copy its **own independent charts and queries** in a single call (the copy shares no chart or query id with the source). `--deep-copy` selects the client-side fallback (`DeepCopyDashboard`): create a new dashboard with the source's filters, then recreate every chart fresh (source charts read in ONE `dashboardCharts:batchGet`; each query read via `GetQuery` and replayed inline through `AddChart`) — also fully independent, for when a server-side copy is unavailable. `dashboards delete` removes a whole dashboard (clean deletes succeed); when the backend returns a 500 it is diagnosed: a **corrupt** dashboard whose `definition.charts` hold dangling/non-owned references (charts owned by another dashboard or already gone) cannot be deleted by the API **or the web console**, the corrupt state can't be repaired first (`:removeChart` 404s; rewriting `definition.charts` 400s), and deleting the chart owner does not unstick the 500 — so it can only be removed by the platform (raise with Google support). Such corrupt dashboards are a legacy artifact, **not** something the current `:duplicate` verb produces.

Portability (verified). `dashboards export <id>` writes a self-contained JSON document — the dashboard plus its charts and queries (the `nativeDashboards:export` `inlineDestination.dashboards[0]` object) — and `dashboards import <file>` re-creates it in ONE `nativeDashboards:import` call (the server mints fresh ids, so an import shares no id with the source). A faster build path than `duplicate` + per-chart `add-chart`, and the way to move a dashboard between instances.

### `curated` / `curated_rules`

Google-managed (no CUD): `pull curated` writes `curated/deployments.yaml`; `push curated` diffs it against live and calls `BatchUpdateCuratedRuleSetDeployments` for changed enabled/alerting tuples.

`curated list` reads deployment state; guarded `curated set` remains the one-off toggle. **Curated read suite (W109):** `curated rule-sets [--category]` lists the curated rule SETS (`ListCuratedRuleSets`, all categories via the `-` wildcard); `curated rules` lists/searches the individual rules with client-side `--search` (name/description), `--set`, `--category`, `--tactic` (MITRE), `--severity` filters; `curated rule <id>` shows one rule's full detail (`GetCuratedRule`). The `CuratedRule` model carries `type`, `precision`, MITRE `tactics`/`techniques`, `description`, and the parent **`curatedRuleSet`** (the rule→set membership — every listed rule has it, so `--set` enumerates a set's rules). Curated YARA-L source is **not** API-exposed (Google-managed); the detail is metadata + MITRE + description.

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

Guarded `rule_exclusions deploy <id> --enable|--disable|--archive` handles one-off toggles.

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

### events (UDM)

Immutable telemetry — **read-only, never mutated**. `query udm` and `query nl` (NL→UDM→search) are built; `stats` was designed then built.

**Query library (W75):** `query run --file <path>|-` runs a UDM predicate from a file or stdin (blank and `#`-comment lines are ignored — `examples/queries/*.udm` run directly). `query saved [<name>]` runs a query by name from a tracked `saved_queries/` pack, or lists it; names are path-sanitized. All reuse the shared `runUDMQuery` core, so window/`--limit`/`--raw`/`--json` semantics match `query udm`.

**`query stats` (W81; aggregation path re-routed in W101):** a plain event search (`query udm`) 400s on a `match:`/`outcome:` aggregation; `query stats '<aggregation YARA-L>'` runs the aggregation and prints the computed columns and rows (`--json` for raw output) — the CLI way to validate the exact YARA-L a dashboard chart uses before authoring. The `match:` section takes a field reference (`target.hostname`), not `$var = field`. Flags parse in any position (interspersed). The aggregation is executed over the **POST `dashboardQueries:execute`** path (`chronicle.ExecuteQuery`) — the same execution `dashboards run-chart` uses; the GET `:udmSearch` stats path (`chronicle.GetStats`) returns `400 INVALID_ARGUMENT` for `match:`/`outcome:` aggregations and is not used for this verb.

### raw logs

Recent **full** raw (ingested) log lines for parser development — two complementary paths, both fetching the complete bytes by raw-log id via `legacyFindRawLogs?ids=` (GET, batches of 25; `logBytes` base64-decoded), one line per log → pipe into `parsers run --logs -`.

**(1) `query udm '<filter>' --raw`** — scopes by UDM metadata (`metadata.log_type`), lifting each event's `udm.metadata.id`; precise, but requires a UDM event. A log type whose parser is missing or broken normalises to **GENERIC_EVENT** (still `parsed = true`), so `metadata.log_type = "<TYPE>" AND metadata.event_type = "GENERIC_EVENT"` returns exactly the logs a parser fix targets; `--limit` defaults to 100.

**(2) `query raw '<regex>'`** — content-based `:searchRawLogs` (`raw = /<regex>/`), matching the raw bytes; reaches even logs with **no parser at all** (no UDM event). `--unparsed` adds `parsed = false`.

Note: the `:searchRawLogs` `logTypes` body filter is **ignored server-side** (confirmed across code/displayName/resource-name forms) — log-type scoping comes from the UDM path; content scoping from the regex. Live read-validated against KONG_GATEWAY (`TestLiveFetchRawLogLines`). SDK: `chronicle/log_search.go`.

### alerts

`alerts list` takes a snapshot over a time window (`legacyFetchAlertsView`, streams a JSON-array of progressive fragments). `alerts get <id>` uses `legacyGetAlert` (response wrapped under `alert`). **Read-verified** (`chronicle/alert.go`; fixed the array-stream decode, the `createdTime`/`detectionTime` keys, and `severityDisplay` being a string).

**Act is CLI-wired (W52):** guarded `alerts update <id>…` sets feedback (status/verdict/priority/reason/reputation/scores/comment/root-cause) over `UpdateAlert`/`BulkUpdateAlerts`; multiple ids fan out. Enum flags accept short forms (`closed`, `false-positive`, `high`) normalised to wire tokens and validated client-side (`AlertUpdate.Validate`) before the guard — dry-run validated; live write gated.

**Alert→case bridge (W52):** `alerts get` resolves the alert's SIEM case UUID to the SOAR integer id (fail-soft). `cases soar-id <uuid…>` is the bulk bridge (`legacyBatchGetCases`) — both read-verified. Operators also read alerts as a **field of the case** via the reliable SOAR lane (`GetCaseFullDetails.alerts`).

**Bulk disposition (W76):** `alerts update --where <snapshot-filter>` resolves a known FP burst to ids (over `--hours`, capped by `--limit`). `--stdin-ids` reads ids on stdin; sources merge and de-duplicate, and the resolved set prints before the guard. A `--where` matching more than `--limit` is a hard error (refuses a silent partial update) — **verified** (dry-run resolves the id set; a too-low `--limit` refuses; the bulk write rides the W52-validated `BulkUpdateAlerts`). `alerts list` surfaces a completeness signal: the pre-filter baseline count and a stderr truncation warning (the alerts snapshot has no server cursor).

### entities

`entity summarize` returns alerts/related/prevalence — enrichment, read-only. Counter fields (`alertCounts.count`, timeline buckets, widget totals, prevalence counts) decode via a tolerant `flexInt` — the API renders int64 counters as JSON strings (proto3 JSON).

### watchlists

SIEM entity watchlists. `watchlists list`/`get <id>` — read-validated. Pinned **v1** (`watchlistsAPIVersion`; all three API versions answer, so v1 is selected).

### analytics & AI reads — investigations, entityRiskScores, bigQueryExport, coverageDetails

SDK: `chronicle/analytics.go` (Wave 17).

Gemini **TIN** investigations (250) and steps are read-validated (list/get/trigger in `investigations.go`). `entityRiskScores:query` (301) returns behavioural risk 0–1000 on v1alpha default. `coverageDetails` returns MITRE coverage (5). `bigQueryExport` get is included. Both `coverageDetails` and `bigQueryExport` are pinned **v1**. `investigationComments` is wired and returns clean typed errors when not provisioned or implemented (501/400, Pre-GA/Enterprise+). Test: `TestLiveAnalyticsRead`. Writes (`:provision`/`update`) are gated, not wired.

### alert AI investigation — alerts investigate

The per-alert Gemini TIN triage flow (W57). `alerts investigate <alert-id>` triggers an investigation, polls to `STATUS_COMPLETED_*`, and prints verdict/confidence/summary/next steps. `--json` adds the embedded investigation steps with the agent's actual UDM queries. `--latest` is the read-only variant (the UI's `alert_id='…' AND latest_in_alert=true` filter, no generation).

The typed `Investigation` carries status/verdict/confidence/summary/nextSteps/notebook/triggerType. `GetNotebook` reads the agent's working document (`notebooks/<id>` — a resource absent from the public REST index). **Live-validated end-to-end** (trigger + filtered list + notebook in `TestLiveInvestigationTriggerRead`; `--latest` render against a completed investigation).

### Gemini chat — query gemini

`query gemini '<question>'` asks SecOps Gemini (YARA-L authoring help, UDM field questions, environment-grounded answers) over the existing `chronicle/gemini.go` conversation flow. **Live-validated (W56).** Live replies carry HTML blocks, rendered as plain prose on the human path (`--json` for the full block structure). `--opt-in` runs the one-time account opt-in.

### findings graph — findingsGraph

Graph-pivot investigation (W56). `InitializeFindingsGraph` seeds a graph from a **detection id** over a time range; `ExploreFindingsGraphNode` expands nodes (node ids are tied to the initialising range). **Read-verified** (`TestLiveWave56Read` — a real detection seed returned root and graph). SDK-only (`chronicle/findings_graph.go`) — the structured "what is connected to this detection" primitive.

### alert enrichment — alerts enrich

`alerts enrich <id>` (read-only, verified) fetches the full per-alert detection collection the console renders when an analyst opens an alert: the rule detection(s), every mapped UDM event, the involved entities/indicators (hosts, users, process path+sha256, domains), the alert's MITRE tags, its SOAR case linkage, and the AI triage verdict when an agent has run. It reads `legacy:legacyBatchGetCollections?collectionIds=<id>` (`chronicle/legacy.go` `BatchGetCollections`, chronicle host / ADC) — the surface the web UI uses. The AI agent's investigation detail is `alerts investigate <id> --latest`.

The earlier `enrichmentAgent:*` path (W56) is a dead end: `fetchAlertData`/`fetchActions`/`executeActions` return **500 INTERNAL** for every variant (across versions and auth forms — AppKey-in-query is a SOAR-host concern, never chronicle) and are **not used by the console**. The pre-case "run an integration action against an alert's entities" verbs that rode it (`actions`/`run-actions`) are therefore withheld rather than shipped always-failing; the SDK methods stay importable (`chronicle/enrichment_agent.go`) for if/when the real surface is captured. The in-case equivalent — `soar case run-action` — works today.

### watchlist membership — watchlists add-entity

`watchlists add-entity <id> (--ip|--mac|--hostname|--user|--email)` puts an entity on a watchlist (`entities:add`, exactly-one-selector contract) — a containment/tracking response action; membership feeds the risk-score multiplier. The request's `entity` is the UDM Entity **envelope** (`{entity: <Noun>}` — the selector sits on the inner Noun, one level below; a flat noun is rejected 400). `RemoveWatchlistEntity` removes by the entity resource name the add response returns (`entities.remove`); `BatchRemoveWatchlistEntities` is SDK-raw. Watchlist CRUD itself (create/delete) is **write-verified**. The membership ops can answer 501 UNIMPLEMENTED per instance — the gated smoke (`TestLiveWatchlistEntityWriteSmoke`, self-contained on a throwaway watchlist) skips cleanly there.

---
