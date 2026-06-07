# Catalog & status

The source of truth for **what exists and how mature it is**. Every surface
carries a status; update it in the same commit that moves the surface forward.
Design in [ARCHITECTURE.md](ARCHITECTURE.md); the full API split by plane and the
unbuilt gaps are in [SURFACES.md](SURFACES.md); product specifics in
[SOAR-DESIGN.md](SOAR-DESIGN.md) / [SIEM-DESIGN.md](SIEM-DESIGN.md).

**Where the code is:** surfaces register in `internal/mirror/registry_{soar,siem}.go`
(playbooks: `soar_playbooks.go`; data_tables: `datatables_surface.go`;
connectors/jobs: `soar_operational_surfaces.go`); the
product-neutral engine is `internal/mirror/reconcile`; live write-smokes are gated by
`SECOPS_SOAR_SMOKE`/`_WRITE` (`reconcile_smoke_test.go`) for SOAR and
`SECOPS_SIEM_SMOKE`/`_WRITE` (`reconcile_smoke_siem_test.go`) for SIEM.

**This catalog has a machine-readable spine.** The status matrix below is mirrored
by the surface-family registry in `internal/mirror/surface_families.go` — one
declarative `SurfaceFamily` entry per API family (`Area`, `Plane`, `Host`, `Auth`,
`Generation`, `APIVersion`, `Lane`, `Status`, `SDKLocation`). SIEM versions in the
registry are sourced from `chronicle/versions.go` (the `APIVersions` map, the single
source of truth for Chronicle-host version pins), and a drift-guard test
(`surface_families_test.go`) asserts the registry, the version pins, and
[ARCHITECTURE.md](ARCHITECTURE.md) §6 stay in agreement.

**Status legend**

| | Status | Meaning |
|:-:|---|---|
| 📐 | **designed** | spec'd, code not landed |
| 🔨 | **built** | code lands |
| ✅ | **live-validated** | reads round-trip clean / a write smoke ran |
| 🔒 | **read-only by choice** | write deliberately not run — RBAC/SSO/high-blast/routing |
| ⛔ | **blocked** | that API path is down server-side (500/404) |
| — | **not used here** | secopsctl doesn't use that API generation for this function |

## How this catalog is sliced

By **function** (cases, rules, playbooks, …) — one row each — grouped into three
areas: **SIEM**, **SOAR**, and cross-cutting **other features** (Threat Intel,
Content Hub). A function can be served by **two API generations**, tracked in
**two columns**, and each column records **status · domain · version**:

- **New API** — Google's modern REST (`projects/…/instances/…` shape). The cell
  reads `<status> <domain> · <version>`, e.g. `✅ chronicle · v1alpha` or
  `✅ siemplify · v1alpha`. **Domain** is a property of the call, not the section —
  the **same function can answer on either domain**. We prefer **v1 > v1beta >
  v1alpha** and pin the version that works per surface (full per-endpoint map:
  [ARCHITECTURE.md](ARCHITECTURE.md) §6).
- **Legacy API** — the Siemplify **external** API (`/api/external/v1`, AppKey, on
  the siemplify domain). SOAR-only; the broad, reliable path the reconcile engine
  and the case verbs run on.

**The two domains** (both are just where the call goes):
- **chronicle** = `chronicle.googleapis.com` (Google), regional, **ADC/OAuth**.
  Serves v1 / v1beta / v1alpha.
- **siemplify** = `*.siemplify-soar.com` (Siemplify), **AppKey**. The modern API
  here is **v1alpha only** (v1/v1beta 404).

A `—` means secopsctl doesn't use that generation for the function. A function is
**not** "blocked" just because one domain/version is down — the status is per
column+domain; if any path serves it, the function works (e.g. **cases** works on
siemplify v1alpha even though the chronicle-host UUID path 500s).

---

## SIEM — Chronicle (`chronicle.googleapis.com`, ADC/OAuth)

Modern-only: the Legacy (Siemplify external) column is `—` throughout — Chronicle
has no legacy external API.

### Control plane — config as code (`pull` → `git diff` → `push`)

| Function (CLI) | Lane | New API (status · domain · version) | Legacy | Notes |
|---|---|---|---|---|
| `rules` | bespoke | ✅ chronicle · v1alpha | — | YARA-L source + deployment state machine (two resources), not a single canonical body. `push rules-create` · `rules-update` (etag-guarded text update) · `rules-deploy` (reconcile enabled/alerting/frequency/**archived**) · `rules-disable`. Operational `rules detections/errors/alerts <id>` + `rules retrohunt list/get/create`. `RunTestRule` (dry-run vs historical data) + `ArchiveRule`. Read live-validated; lifecycle write smoke `TestLiveRulesLifecycleWriteSmoke` (create→update→deploy→archive→delete, self-cleaning) |
| `reference_lists` | reconcile | ✅ chronicle · v1alpha | — | typed, `.txt`+`.yaml`; NoDelete; product-neutral engine. Resource-name **normalization**: create echoes the project NUMBER while list echoes the project ID — both rewritten to the id form so reconcile identity (keyed on the name) stays stable. Write smoke `TestLiveReconcileReferenceListWriteSmoke` reuses one fixed inert list (no delete API) — fresh create-or-reuse + one update each run (rerunnable, no accumulation) |
| `data_tables` | reconcile | ✅ chronicle · v1alpha | — | `.csv`+`.yaml` on the engine; `push data_tables` (create/update). Columns immutable after create; rows are wholesale destroy-and-replace (`ReplaceDataTableRows`). Not prune-eligible (whole-table delete is high-blast). Write smoke `TestLiveReconcileDataTableWriteSmoke` (create→update desc→replace rows→delete) |
| `feeds` | reconcile | ✅ chronicle · v1alpha | — | `.yaml` on the engine; `push feeds`. Secrets redacted on pull, overlaid on update (real secret preserved; create refuses a masked body); `details` replaced wholesale on PATCH. `assetNamespace`(read) vs `namespace`(write) reconciled (API uses `assetNamespace`); short `logType` expanded to the full resource name on write. Feed state is a runtime toggle, out of canonical. Not prune-eligible (delete stops ingestion). Write smoke `TestLiveReconcileFeedWriteSmoke` (inert HTTP throwaway, create→update→delete); GCS V2 (`gcsV2Settings`, STS-backed) validated; `FetchFeedServiceAccount` for the STS SA grant |
| `parsers` | reconcile | ✅ chronicle · v1alpha | — | `.conf`+`.yaml` on the engine; `push parsers`. Versioned/immutable → no server-side update: an edit is **create-new-version + activate** (parser id volatile, written back on refresh); old version left inactive (rollback). Not prune-eligible. Write smoke `TestLiveReconcileParserWriteSmoke` runs `RunParser` (pure inert validation) then creates a new **INACTIVE** version, asserts it never goes ACTIVE (live ingestion untouched), deletes it. `RunParser` response shape: `parsedEvents` is `{events:[…]}` |
| `dashboards` | reconcile | ✅ chronicle · v1alpha | — | native dashboards (**CUSTOM only**; CURATED read-only/unmanaged); `pull`/`push dashboards`. One `<slug>.json` (config + `_server` id), charts inline under `definition.charts` (replaced wholesale on update); `access` immutable after create. extraStrip drops `createUserId`/`updateUserId`/`dashboardUserData`; root `name` stripped (identity in ServerID). Write smoke (create→update→delete, closure-direct to dodge full-view rate-limiting) |
| `curated` / `curated_rules` | imperative (read+toggle) | ✅ chronicle · v1alpha | — | Google-managed (no CUD) → `curated list` + guarded `curated set` toggling `enabled`/`alerting` per (category, rule set, precision). `curated rules` lists individual curated rules (`ListCuratedRules`/`GetCuratedRule`, read-validated, 187 rules); `BatchUpdateCuratedRuleSetDeployments` is the atomic multi-deployment write primitive (live-validated by `TestLiveCuratedBatchToggleWriteSmoke` — a self-restoring enable→verify→restore toggle on an inert deployment, alerting off). Imperative lane (fixed catalog, array batch body), not reconcile. (v1alpha is the **only** version that answers — v1/v1beta 404) |
| `rule_exclusions` | reconcile | ✅ chronicle · v1alpha | — | findings refinements (display_name + type + UDM query); `pull`/`push rule_exclusions`. Create + Update (PATCH, updateMask); **NoDelete** (drift reported, never pruned), NoEtag. Deployment toggle (enabled/archived) out of the diff basis. Read + write live-validated (create→update→archive); the API has no hard delete — **archive** is the teardown |
| `forwarders` | reconcile | ✅ chronicle · v1beta | — | `.yaml` on the engine; `pull`/`push forwarders`. Diff basis is `display_name` + the freeform `config` block (uploadCompression, metadata, serverSettings, …); runtime `state` and server-stamped times stripped from the canonical. Config replaced wholesale on PATCH so Update overlays local edits onto the live body. NoEtag; **prune-eligible** (clean delete-by-id). Collectors are a separate nested resource. SDK pinned v1beta (v1 **404s**). Write smoke `TestLiveReconcileForwarderWriteSmoke` (inert throwaway, serverSettings disabled, create→update config→delete, self-cleaning) |
| `metric_definitions` | reconcile | 🔨 chronicle · v1alpha | — | custom SOC metrics (id = display name; `text_definition` is **YARA-L 2.0**); `pull`/`push metric_definitions`. **Additive** (create + state-only patch; **textDefinition immutable** — a text edit is refused, change = new id; **no delete API** → NoDelete). One `<slug>.yaml` (display_name, name, state, text_definition). Built + offline-tested; **live read is 403 on this tenant — the feature is not enabled/GA** (Chronicle admin still blocked), so it is not live-validated here. `chronicle/metrics.go` |
| `scheduled_reports` | reconcile | 🔨 chronicle · v1alpha | — | scheduled dashboard reports (`dashboardScheduledReports`): recurring PDF/CSV/PNG delivery of a native dashboard on a cron, full CRUD with etag; `pull`/`push scheduled_reports`. One `<slug>.json` (config + `_server` id/etag); the embedded `dashboard` is reduced to its `{name}` reference (the dashboard is managed separately). **Prune-eligible** (clean delete-by-id). Imperative `trigger`/`duplicate`/`fetchHistory` in the SDK. **Reads live-validated** (list 200); the create-report backend currently **500s "failed to fetch native dashboard details"** (server-side — the `{name}` ref shape is accepted/parsed; verified for existing + new dashboards, both project forms), so the write-smoke (`TestLiveReconcileScheduledReportWriteSmoke`) skips on that 500. `chronicle/scheduled_reports.go` |
| `datataps` | reconcile | ✅ chronicle · v1alpha | — | stream UDM events to a Cloud Pub/Sub topic (`dataTaps`): `pull`/`push datataps`. One `<slug>.yaml` (display_name, name, filter ALL/ALERT/LABELED, serialization_format JSON_OBJECT/MARSHALLED_PROTO defaulted, topic). **Prune-eligible**; NoEtag. **Write live-validated** (`TestLiveReconcileDataTapWriteSmoke`, create→update→delete on an inert tap pointed at a nonexistent topic). PATCH is **501 UNIMPLEMENTED** on the backend, so an update is done as **delete-old + create-new** (the id is server-assigned and changes); `UpdateDataTap` is kept for when PATCH lands. Supersedes the legacy Backstory `dataTaps`. Prereq for a live tap: grant Pub/Sub Publisher to `publisher@chronicle-data-tap.iam.gserviceaccount.com`. `chronicle/datataps.go` |
| `error_notifications` | reconcile | 🔨 chronicle · v1alpha | — | ingestion-health alerts (`errorNotificationConfigs`): zero-ingest / size-threshold / normalization-delay → Cloud Monitoring channels; `pull`/`push error_notifications`. One `<slug>.json` (displayName, enabled, notificationChannels + one oneof notification_type block kept raw) + `_server` id. Full CRUD, **prune-eligible**, NoEtag; updateMask derived from present keys (the oneof masks as `notification_type`). Built + offline-tested; **feature-gated 403** on the tenant, so not live-validated. `chronicle/error_notifications.go` |
| `enrichment_controls` | imperative | 🔨 chronicle · v1alpha | — | turn OFF a UDM enrichment per log type / enrichment type (`enrichmentControls`). SDK `ListEnrichmentControls`/`Get`/`Create`/`Disable`/`Delete` (`chronicle/enrichment_controls.go`). **Imperative, not reconcile** — there is no patch, a create for an existing control appends a time-ranged record, and `:disable` closes the latest record, so config-as-code round-tripping doesn't fit. Built + read-attempted; **feature-gated 403** on the tenant. |
| schema discovery — `feedSourceTypeSchemas` · `logTypeSchemas` · `logTypeSetting` · `logTypes.get` | imperative (read) | ✅ chronicle · v1alpha | — | SDK (`chronicle/schemas.go`): `ListFeedSourceTypeSchemas` (the available feed source types), `ListLogTypeSchemas(sourceType)` (accepted log types + required detail fields per source type), `GetLogTypeSetting` (per-log-type ingestion config), and `GetLogType` (single log type — a documented v1alpha method). The reference for validating feed YAML before a deploy. Read-only (upstream-defined). Rides the feeds family — project ID form, v1alpha default. Read-validated live (`TestLiveSchemaDiscoveryRead`). Per-log-type GET (`logTypes.get`) is wired but is a documented method some instances don't enable — it can 404 "Method not found" across all versions and both hosts; in that case enumerate with `ListLogTypes`. Wire into feed validation |
| governance — `riskConfig` · `dataAccessLabels`/`Scopes` | imperative | ✅ chronicle · v1 | — | SDK (`chronicle/rbac.go`, `rbacAPIVersion`; all three versions answer → pinned v1), **write-validated (Wave 10)**: **dataAccessLabels** CRUD (`TestLiveDataAccessLabelWriteSmoke`), **dataAccessScopes** CRUD (`TestLiveDataAccessScopeWriteSmoke` — a throwaway, unassigned scope allowing a throwaway label), and **riskConfig** GET + idempotent `UpdateRiskConfig` (`TestLiveRiskConfigWriteSmoke`, same-value) — all self-cleaning on inert throwaways. Create-body shapes: a label needs a `udmQuery`; a scope sets `allowAll` + `allowed/deniedDataAccessLabels:[{dataAccessLabel:<labelId>}]`. The surface is quirky — **create can return an error yet still persist**, **create→list lags**, deleted ids **tombstone**, body `displayName` ignored — so it is operated **imperatively** (unique ids + delete-by-exact-id), not via the reconcile engine (list lag breaks diffing). No CLI wired yet |

### Operational plane — query → act (live data)

> **Cases are a SOAR function** — see **SOAR → cases** below (working path:
> siemplify · v1alpha). A `cases` CLI command reaches the *same* case on the
> **chronicle** host by UUID (ADC), but that path 500s at every version, so prefer
> `soar case`. Tracked in the cases row's notes, not as a separate blocked surface.

| Function (CLI) | Lane | New API (status · domain · version) | Legacy | Notes |
|---|---|---|---|---|
| **events (UDM)** | operational (read) | 🔨 chronicle · v1alpha (`query udm`) · 📐 rest | — | immutable telemetry — **read-only, never mutated**. `query udm` built; `search nl` / `stats` designed |
| **alerts** | operational | ✅ read · 🔨 act | — | `alerts list` (snapshot over a time window — `legacyFetchAlertsView`, streams a JSON-array of progressive fragments) + `alerts get <id>` (`legacyGetAlert`, response wrapped under `alert`) — **read-validated live** (`chronicle/alert.go`; fixed the array-stream decode, the `createdTime`/`detectionTime` keys, and `severityDisplay` being a string). Act (`UpdateAlert`/`BulkUpdateAlerts` feedback) built, gated, not run. Operators also read alerts as a **field of the case** via the reliable SOAR lane (`GetCaseFullDetails.alerts`) |
| **entities** | operational (read) | 📐 chronicle · v1alpha | — | `entity summarize` — enrichment, read-only |
| **watchlists** | operational (read) | ✅ chronicle · v1 | — | SIEM entity watchlists; `watchlists list`/`get <id>`, read-validated, pinned **v1** (`watchlistsAPIVersion`; all three answer → v1) |
| **analytics & AI reads** — `investigations`(+steps/comments) · `entityRiskScores` · `bigQueryExport` · `coverageDetails` | operational (read) | ✅ chronicle (Wave 17, `chronicle/analytics.go`) | — | Gemini **TIN** investigations (250) + steps read-validated (list/get/trigger in `investigations.go`); `entityRiskScores:query` (301, behavioral risk 0–1000); `coverageDetails` MITRE coverage (5) — both pinned **v1**; `bigQueryExport` get + `investigationComments` wired, return clean typed errors when not provisioned/implemented (501/400, Pre-GA/Enterprise+). `TestLiveAnalyticsRead`. Writes (`:trigger`/`:provision`/`update`) gated, not wired. SDK-only (no CLI yet) |

---

## SOAR — Siemplify (`*.siemplify-soar.com`, AppKey)

Two API generations on this domain: **New** = modern v1alpha (same
`projects/…/instances/…` shape as SIEM, **v1alpha only** — v1/v1beta 404);
**Legacy** = the external `/api/external/v1` API — the broad, reliable path. The
reconcile engine and the case verbs run on **Legacy**; secopsctl reaches for New
only where it's validated and adds something Legacy lacks. `--legacy` forces Legacy.

### Control plane — config as code (`soar pull` → `git diff` → `soar push`)

All reconcile surfaces run on the **Legacy** engine (reliable). A modern v1alpha
twin exists on the SOAR domain for most; the reconcile engine doesn't route to it
yet (`—`), but the v1alpha **writes are validated** for several config surfaces
(create→get→delete on customLists/socRoles/caseTagDefinitions; environments create
reachable but license-capped) — they do **not** 500 (`TestLiveConfigSurfaceWriteSmoke`),
so `—` is a routing choice, not an API gap.

| Function | Lane | New API | Legacy (siemplify · external) | Notes |
|---|---|---|---|---|
| `webhooks` | reconcile | — | ✅ | full CUD; create is license-capped (engine surfaces it, smoke skips). **PruneEligible** |
| `environments` | reconcile | ✅ siemplify · v1alpha (create/get/update/delete wired; create reachable — license-capped) | 🔒 | modern write endpoint validated (does not 500) via `TestLiveConfigSurfaceWriteSmoke`; legacy reconcile NoDelete (segregation unit — high blast), writes guarded |
| `networks` | reconcile | ✅ siemplify · v1alpha (`soarNetworks` — CRUD wired `soar/data_surfaces.go`; create→get→delete **write-validated** `TestLiveDataSurfaceWriteSmoke`; +`deleteAll`/`export`/`import` documented) | ✅ | write smoke (RFC5737 throwaway). **PruneEligible**: `DeleteNetwork(id)` is a clean by-id delete (`TestLiveReconcileNetworkDeleteByIDSmoke`); low-blast enrichment data |
| `tracking-lists` | reconcile | — | ✅ | first write-loop proof (clone throwaway) |
| `soc-roles` | reconcile | ✅ siemplify · v1alpha (CRUD wired; create→get→delete live-validated, `TestLiveConfigSurfaceWriteSmoke`) | ✅ | RBAC. Modern write does not 500. Legacy write **path** validated via an inert throwaway role — create→rename→delete (no users assigned); `DeleteSocRole` takes `{socRoleId}`. **Engine-NoDelete** (delete via raw SDK only; `--prune` never deletes these) — reconcile RBAC with care |
| `idp` | reconcile | — | ✅ | SSO; id-from-body update closure. Write **path** validated via a throwaway mapping for a **fake group** (no real users) — create→rename→delete-by-id. **Engine-NoDelete** — reconcile SSO with care |
| `visual-families` | reconcile | — | ✅ | write smoke; validates the `wrapKey` envelope. **PruneEligible**: `DeleteFamilyData` is a clean by-id delete on an inert custom family |
| `sla-definitions` | reconcile | ✅ siemplify · v1alpha (`slaDefinitions` — CRUD wired; create→get→delete **write-validated** `TestLiveDataSurfaceWriteSmoke`; **string** enums `SlaType`/`SlaTimeUnit` not the legacy ints; +`export`/`import`) | ✅ | affects alert routing. Write **path** validated via a throwaway "Case Priority = High" SLA. Legacy `ApiSlaDefinition` int enums are documented in the **swagger schema `description`** fields: `valueType` (`ApiSlaProviderTypeEnum`) 2=AlertRuleGenerator/3=CaseStage/**4=CasePriority**/5=AlertPriority; `slaPeriodType`/`criticalPeriodType` (`ApiPeriodTypeEnum`) 0=Minutes/**1=Hours**/2=Days/3=Seconds; `alertType` (`ApiSlaAlertType`) **0=AllAlerts**/1=SpecificAlerts. For CasePriority the `value` round-trips as a JSON-array string (`["High"]`). (The v1alpha twin uses string enums; Legacy is the reliable one.) **Engine-NoDelete** (delete via raw `RemoveSlaDefinitionRecords`) — routing surface, reconcile with care |
| `case-stages` | reconcile | ✅ siemplify · v1alpha (caseStageDefinitions create/delete wired) | ✅ | wrapped list. Modern create/delete wired (family validated; the v1alpha case-defs do not 500). Legacy write **path** validated via an inert throwaway stage — create→reorder→delete (used by no case); `RemoveCaseStageDefinitionRecords` takes the full record. **Engine-NoDelete** — UI-pollution, reconcile with care |
| `case-tags` | reconcile | ✅ siemplify · v1alpha (caseTagDefinitions create/get/delete **live-validated**, `TestLiveConfigSurfaceWriteSmoke`) | 🔨 | modern create→get→delete works (does not 500; `priority` must be > 0). Legacy write smoke skips (no tag to clone) |
| `close-root-causes` | reconcile | ✅ siemplify · v1alpha (caseCloseDefinitions create/delete wired) | ✅ | modern create/delete wired (case-defs family validated, no 500). Legacy: non-unique names → exercises the slug-collision fix |
| `blacklists` | reconcile | 🔨 siemplify · v1alpha (`entitiesBlocklists` — read + create/get/delete wired `soar/data_surfaces.go`; writes reach the endpoint (HTTP 400, **not** 500) but `action` (`ActionScope`) + `entityType` are undocumented server enums — supply a valid token; not write-validated) | ✅ | model block-list; write smoke |
| `playbook-categories` | reconcile | — | ✅ | write smoke |
| `playbooks` | reconcile (bespoke) | — | ✅ | uuid rotates → key on **name**; whole-body save; SavePlaybook update-by-name verified. (Playbooks/workflows exist **only** on Legacy — no New twin) |
| `connectors` | reconcile | — | ✅ | moved onto the reliable Legacy engine (replaces a former modern v1alpha pull+patch). **Full CUD** — create + whole-body update (`SaveConnector`) + delete-by-id (`DeleteConnector`, **PruneEligible**). `SaveConnector` upserts both: **create** triggers when the body has **no `identifier`** (server assigns one); a client-assigned id routes to update (404 if absent) — a new connector file omits `identifier`, so engine create works. `ListConnectorCards` groups cards by integration, so the list closure **flattens** them. Secret params arrive server-masked and pass through unchanged on update. extraStrip = `version`/`isUpdateAvailable`/`loggingEnabledUntilUnixMs`/`isCustom`. `TestLiveReconcileConnectorWriteSmoke` (throwaway DISABLED connector → create → update → delete; self-cleaning) |
| `jobs` | reconcile | — | ✅ | Legacy engine (replaces a former modern v1alpha pull+patch). pull + update (`SaveOrUpdateJob` whole-body upsert; the installed-job read item IS the write body); **NoDelete** (delete takes a body, not a clean id). extraStrip drops `version`/`lastRunStatus`/`lastRunTime`/`creator`. `TestLiveReconcileJobWriteSmoke` (throwaway DISABLED job → update → raw delete; self-cleaning) |
| `grouping` | raw (modern) | ✅ siemplify · v1alpha | — | alert-grouping rules + module settings on the v1alpha SOAR host. **Full rule lifecycle write-validated** — create→get→delete on a self-cleaning inert throwaway (`TestLiveAlertGroupingRuleWriteSmoke`); list/get/patch + module settings batch-update. Numeric `id` decoded as number-or-string. Pull + patch is raw (not full reconcile) |
| `case-data` — `customFields` · `calculatedFieldDefinitions` · `propertySchemaDefinitions` | imperative (modern) | ✅ siemplify · v1alpha — SDK full CRUD (`soar/case_data_surfaces.go`); create→get→delete **write-validated** (`TestLiveCaseDataWriteSmoke`) | — | Wave 16. customFields `scopes`="Case"/"Alert" (FREE_TEXT needs no options; **"All" 500s**); calc is logic-as-code (`SET_VALUE`/`TEXT`/`targetField=CaseCustom.<field>`/`formulaExpression="…"`) and depends on a Free-Text custom field (create field→calc, delete calc→field). SDK-only so far — no CLI wired yet |

### Operational + imperative — query → act / per-entity verbs

| Function (CLI) | Lane | New API (status · domain · version) | Legacy (siemplify · external) | Notes |
|---|---|---|---|---|
| `soar case list` / `get <id>` | operational read | ✅ siemplify · v1alpha (`list` default) | ✅ (`get`; `list` fallback) | **cases work on the New API** — `list` defaults to the modern v1alpha cases API on the siemplify domain (`soar.ListCases`, live-validated) and **auto-falls back to the Legacy queue** (`ListCaseCards`) on error; **`--legacy`** forces Legacy. `list` sends the web-UI query params — server-side `filter` (`--status` → `status = 'OPENED'`/`'CLOSED'`), `orderBy=updateTime desc`, and `expand` (products/tasks/tags/closureDetails/sla/alertsSla) via `ListCasesOpts` (client-side status re-check kept as a safety net); live-validated (open vs closed return different counts). `get <id>` uses Legacy `GetCaseFullDetails` → case **and its alerts** (each with its `--alert` id for the verbs). Shared `preferModern` helper. **Alternate path (not used):** the same case is reachable on the **chronicle** host by UUID (ADC), but that collection 500s at v1/v1beta/v1alpha — the `cases` CLI routes there, so prefer `soar case`. `legacyBatchGetCases` bridges SOAR int id ⇄ SIEM UUID |
| `soar case <verb>` (assign/rename/stage/tag/untag/describe/importance/close/merge) | imperative | — | ✅ | **the reliable operational case path**. 9 mutate verbs; swagger-verified bodies + unit test; live-validated end-to-end by `TestLiveSOARCaseVerbsWriteSmoke` (create two throwaway cases → run every verb → merge → close). Built on a typed `CreateManualCase` (`ManualCaseRequest`, returns the new case id) that always sends `entities`/`playbooks`/`tags` as `[]` (the server does not null-guard them). `merge` needs the target id in `casesIds` (CLI adds it). `assignedUser` takes `@RoleName`. Hard delete (`RetentionDeleteCases`) is denied to the AppKey role (403) → smoke cleans up by **closing** |
| `soar push bulk-close` | imperative | — | 🔨 | queue bulk-close (`ExecuteBulkCloseCase`). Takes a **fixed reason enum** (malicious/not-malicious/maintenance/inconclusive/unknown), unlike single-case `close` (free string) |
| `soar settings case-assignment` / `move-case-policy` (`get`/`set`) | imperative | — | 🔨 | singleton case-routing policies (one record, no id/list/delete) → imperative get/set, not reconcile. `get` read-only; `set <enum>` guarded |
| `form-dynamic-parameters` | deferred | 🔒 siemplify · v1alpha (unsafe PUT) | 🔒 (read) | investigated as a reconcile surface but **not wired** — the strict PUT update silently resets a parameter's `formType` to Invalid (dropping it from its form) even with the int-enum body the UI sends. Read-only via `soar legacy call settings/form-dynamic-parameters?formType=CloseCase` |
| `soar legacy call <op>` | raw | — | ✅ | passthrough for integrations · ontology-mapping (selector read + batch upsert + body delete; the canonical raw-lane case) · environment-priorities · permissions/SSO (read-only by choice) · system/singleton settings · … (batch/bundle/selector) |

---

## Other features — cross-cutting (domain varies per row)

Grouped by feature, not by domain. The New-API cell names the domain because these
span both Google (chronicle) and Siemplify (siemplify).

### Threat Intelligence (Mandiant / Emerging Threats)

| Function (CLI) | Lane | New API (status · domain · version) | Legacy | Notes |
|---|---|---|---|---|
| `ti collections` / `collection <id>` | operational read | ✅ chronicle · v1 | — | Mandiant `threatCollections` (campaigns/reports/actors/malware/vuln) — list (`collection_type:` filter + orderBy + `--limit`) + get-by-id, read round-trip live-validated (`chronicle/ti.go`, `TestLiveThreatCollectionsRead`). Read-only (upstream-sourced). Prefer v1 > v1beta > v1alpha; all three answer → pinned **v1** (`tiAPIVersion`); threatCollections uses the project **number** |
| IoCs — `iocs find` / `iocs get` | operational read | ✅ chronicle · v1 | — | modern IoC lookup, read-validated. `iocs find <value>` resolves indicators via the `fieldAndValue` body (`{value, valueType}`, type auto-detected for hash/domain/IP or `--type`), pinned **v1**; `iocs get <id>` fetches one record. SDK `FindIoCs`/`GetIoC`/`BatchGetIoCs` (`chronicle/ti.go`) |

### Content Hub & integrations

Installing content (integration **packages** and the connector/job/action
**definitions** they carry) and the marketplace catalog. Configured integration
**instances** are environment-scoped and operated imperatively. All on the
**siemplify** domain.

| Function (CLI) | Lane | New API (status · domain · version) | Legacy (siemplify · external) | Notes |
|---|---|---|---|---|
| `soar marketplace list` / `get` / `contentpacks` | imperative read | ✅ siemplify · v1alpha | — | Content Hub reads (`soar/marketplace.go`) — `list [--installed]` (405 integrations) + `get <id>` + `contentpacks` (59). Read-validated. **Install/uninstall live-validated** (Wave 11, `TestLiveMarketplaceInstallWriteSmoke` — install→verify→uninstall round-trip on an inert, not-installed utility pack, self-cleaning; reversible via the modern `:uninstall`). SDK-only (no CLI for the mutations — heavy, deliberate ops) |
| `soar integration list` / `uninstall` | imperative | ✅ siemplify · v1alpha | — | `list [--custom] [--json]` enumerates installed packs (`soar.ListIntegrations`); `uninstall --name <key>` deletes a **custom** pack (`soar.DeleteIntegration`, guarded). `soar.IsDeletableIntegration` = `custom:true` only — refuses commercial/installed packs. Read live-validated |
| `soar integration connector list` / `delete` | imperative | ✅ siemplify · v1alpha | — | connector **definitions** (templates inside a pack; distinct from the configured connector *instances* in the SOAR reconcile table). `list --integration <key>` (`soar.ListConnectors`); `delete --integration <key> --id <connId>` (`soar.DeleteConnectorDef`, guarded). Only `custom:true` definitions are deletable — removes a duplicated **"Copy of …"** connector without touching the stock one. Read + delete live-validated |
| `soar integration create` / `delete` (instances) | imperative | — | 🔨 | integration **instances** are not reconcilable (no update endpoint; create body doesn't round-trip from any read) → imperative. `create --integration <id> --environment <env>` (new instance starts unconfigured/inert); `delete --integration <id> --environment <env> --id <instance>` (resolves the full object — delete takes a body — and warns if playbooks use it). `TestLiveIntegrationInstanceCRUD`; guarded |
| integration **install** lifecycle (SDK only) | imperative/raw | 🔨 siemplify · v1alpha (`:install`/`:uninstall`) | 🔨 (`/store`) | install a pack + its connector/job/action **definitions**. Legacy `legacy.GetPackageDetails` + `legacy.DownloadAndInstallIntegration` (`/api/external/v1/store/…` — **not in the swagger snapshot**, shape from the live Content-Hub request) install from the local store; the v1alpha `marketplaceIntegrations:install`/`:uninstall` is the documented twin — **live-validated and cleanly reversible** (install→uninstall round-trip leaves no residue). Whole-integration delete is v1alpha-only (`integrations.delete`) and limited to genuinely **custom** packs (`custom:true`); per-tenant installed copies carry a `__<uuid>` suffix but are `custom:false`, so they are not whole-deletable. The **legacy `/store`** install path is the one with no clean reverse — prefer the modern `marketplaceIntegrations` pair |

---

## How to keep this current

When a surface advances: edit its row here **and** the relevant design doc in the
**same commit**. A surface reaches `✅` only after a live read round-trips clean and
(for writes) a gated smoke passed on an inert throwaway — see the build discipline
in [ARCHITECTURE.md](ARCHITECTURE.md) §5.

A `⛔` belongs to a **specific column + domain + version** that's down — never to a
whole function. If the function works on *any* path (another domain or version),
its row stays green and the dead path is a **note** (as with cases: ✅ on siemplify
v1alpha; the chronicle-host UUID path 500s, noted, not blocking). When the working
version of a Chronicle-host New-API surface moves, change the pin in
`chronicle/versions.go` (the `APIVersions` map), then update the cell's
`domain · version` here and the §6 table in [ARCHITECTURE.md](ARCHITECTURE.md). The
surface-family registry (`internal/mirror/surface_families.go`) reads its SIEM
versions from that map, and the drift-guard test fails if the three fall out of sync.
