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

**Status legend**

| | Status | Meaning |
|:-:|---|---|
| 📐 | **designed** | spec'd, code not landed |
| 🔨 | **built** | code lands |
| ✅ | **live-validated** | reads round-trip clean / a write smoke ran |
| 🔒 | **read-only by choice** | write deliberately not run — RBAC/SSO/high-blast/routing |
| ⛔ | **blocked** | Google API down |
| — | **n/a** | — |

## SOAR · control plane (reconcile)  — auth: AppKey (reliable)

| Surface | Lane | Identity | Read | Write | Notes |
|---|---|---|---|---|---|
| `webhooks` | reconcile | `identifier` | ✅ | 🔨 | full CUD; create is license-capped (engine surfaces it, smoke skips) |
| `environments` | reconcile | `id` | ✅ | 🔒 | NoDelete (segregation unit — high blast); writes guarded, not run |
| `networks` | reconcile | `id` | ✅ | ✅ | write smoke (RFC5737 throwaway). **PruneEligible** (Wave 7): `DeleteNetwork(id)` is a clean by-id delete (record id == DELETE path id, confirmed by `TestLiveReconcileNetworkDeleteByIDSmoke`); low-blast enrichment data |
| `tracking-lists` | reconcile | `id` | ✅ | ✅ | first write-loop proof (clone throwaway) |
| `soc-roles` | reconcile | `id` | ✅ | ✅ | RBAC. Write **path** live-validated (Wave 7) via an inert throwaway role — create→rename→delete (no users assigned); `DeleteSocRole` takes `{socRoleId}`. **Operationally additive / engine-NoDelete** (delete via raw SDK only; `--prune` never deletes these) — reconcile RBAC with care |
| `idp` | reconcile | `id`(uuid) | ✅ | ✅ | SSO; id-from-body update closure. Write **path** live-validated (Wave 7) via a throwaway mapping for a **fake group** (no real users) — create→rename→delete-by-id. **Operationally additive / engine-NoDelete** — reconcile SSO with care |
| `visual-families` | reconcile | `id` | ✅ | ✅ | write smoke; validates the `wrapKey` envelope. **PruneEligible** (Wave 7): `DeleteFamilyData` is a clean by-id delete on an inert custom family |
| `sla-definitions` | reconcile | `id` (name=`value`) | ✅ | ✅ | affects alert routing. Write **path** live-validated (Wave 7) via a throwaway "Case Priority = High" SLA — engine create→update→delete. Legacy `ApiSlaDefinition` int enums are documented in the **swagger schema `description`** fields: `valueType` (`ApiSlaProviderTypeEnum`) 2=AlertRuleGenerator/3=CaseStage/**4=CasePriority**/5=AlertPriority; `slaPeriodType`/`criticalPeriodType` (`ApiPeriodTypeEnum`) 0=Minutes/**1=Hours**/2=Days/3=Seconds; `alertType` (`ApiSlaAlertType`) **0=AllAlerts**/1=SpecificAlerts. For CasePriority the `value` round-trips as a JSON-array string (`["High"]`). (The v1alpha UI endpoint uses string enums; the legacy AppKey path is the reliable one.) **Operationally additive / engine-NoDelete** (delete via raw `RemoveSlaDefinitionRecords`) — routing surface, reconcile with care |
| `case-stages` | reconcile | `id` | ✅ | ✅ | wrapped list. Write **path** live-validated (Wave 7) via an inert throwaway stage — create→reorder→delete (used by no case); `RemoveCaseStageDefinitionRecords` takes the full record. **Operationally additive / engine-NoDelete** — UI-pollution, reconcile with care |
| `case-tags` | reconcile | `id` | ✅ | 🔨 | wrapped list; write smoke skips (no tag to clone) |
| `close-root-causes` | reconcile | `id` | ✅ | ✅ | non-unique names → exercises the slug-collision fix |
| `blacklists` | reconcile | `id` | ✅ | ✅ | model block-list; write smoke |
| `playbook-categories` | reconcile | `id` | ✅ | ✅ | write smoke |
| `playbooks` | reconcile (bespoke) | **name** | ✅ | ✅ | uuid rotates → key on name; whole-body save; SavePlaybook update-by-name **verified** |
| `connectors` | reconcile | `identifier` | ✅ | ✅ | Wave 7: moved onto the reliable legacy AppKey engine (replaces the modern v1alpha pull+patch). **Full CUD** — create + whole-body update (`SaveConnector`) + delete-by-id (`DeleteConnector`, **PruneEligible**). `SaveConnector` is the upsert for both: the **create** path triggers when the body has **no `identifier`** (server assigns one); sending a client-assigned id routes to the *update* path (404 for an id that doesn't exist yet) — a new connector file naturally omits `identifier`, so engine create works (operator supplies the mandatory params). `ListConnectorCards` groups cards by integration (`[{integration, cards:[…]}]`), so the list closure **flattens** them. Secret params arrive server-masked (`***…`) and pass through unchanged on update. extraStrip = `version`/`isUpdateAvailable`/`loggingEnabledUntilUnixMs`/`isCustom` (the volatile server-managed fields on the `GetConnector` body). Covered by `TestLiveReconcileConnectorWriteSmoke` (throwaway DISABLED connector from a template → engine create → update → delete; iterates templates + fills mandatory params; self-cleaning) |
| `jobs` | reconcile | `uniqueIdentifier` | ✅ | ✅ | Wave 7: legacy AppKey engine (replaces modern v1alpha pull+patch). pull + update (`SaveOrUpdateJob` whole-body upsert; the installed-job read item IS the write body); **NoDelete** (delete takes a body, not a clean id). extraStrip drops `version`/`lastRunStatus`/`lastRunTime`/`creator` (and `lastModificationTime`, now a global timekey). **Read + write live-validated** by `TestLiveReconcileJobWriteSmoke` (throwaway DISABLED job from a template → engine update → raw delete; self-cleaning) |

## SOAR · imperative + raw

| Surface | Lane | Status | Notes |
|---|---|---|---|
| `soar case list` / `get <id>` | operational read | ✅ | **the reliable operational read path** (AppKey), live read-validated. `list` (`ListCaseCards`; `--status`/`--limit`/`--json`) + `get <id>` (`GetCaseFullDetails` → case **and its alerts**, each with its `--alert` identifier for the mutate verbs); table or `--json` |
| `soar case <verb>` (assign/rename/stage/tag/untag/describe/importance/close/merge) | imperative | ✅ | **the reliable operational case path** (AppKey). 9 mutate verbs; swagger-verified bodies + unit test; live-validated end-to-end by `TestLiveSOARCaseVerbsWriteSmoke` (create two throwaway cases → run every verb → merge → close). Built on a now-typed `CreateManualCase` (`ManualCaseRequest`, returns the new case id): the earlier 500 was a **server NPE on null collections** — the legacy server does not null-guard `entities`/`playbooks`/`tags`, so omitting them threw a post-creation 500 (the case was still created; the transport's retry-on-500, since fixed, then duplicated it). The SDK now always sends those as `[]`. `merge` requires the target id to be present in `casesIds` (the CLI now adds it automatically). `assignedUser` takes `@RoleName`. Hard delete (`RetentionDeleteCases`) is denied to the AppKey role (403), so the smoke cleans up by **closing** (re-run-tolerant, not zero-residue without a retention grant) |
| `soar push bulk-close` | imperative | 🔨 | queue bulk-close (`ExecuteBulkCloseCase`, AppKey) — pre-existing |
| `soar integration create` / `delete` | imperative | 🔨 | Wave 7: integration **instances** are not reconcilable (no update endpoint; create body doesn't round-trip from any read), so they are operated imperatively. `create --integration <id> --environment <env>` (new instance starts unconfigured/inert); `delete --integration <id> --environment <env> --id <instance>` (resolves the full instance object — delete takes a body — and warns if playbooks use it). Covered by `TestLiveIntegrationInstanceCRUD`; guarded |
| integration **install** lifecycle (SDK only) | imperative/raw | 🔨 | Installing an integration PACKAGE (and the connector/job/action **definitions** it carries) is API-driven, separate from instances: `legacy.GetPackageDetails` + `legacy.DownloadAndInstallIntegration` (`/api/external/v1/store/…`, AppKey — **not in the swagger snapshot**, shape from the live Content-Hub request) install from the local store; the modern v1alpha `marketplaceIntegrations:install`/`:uninstall` is the documented twin. Whole-integration delete is v1alpha-only (`integrations.delete` → `soar.DeleteIntegration`) and limited to genuinely **custom** packs (`custom:true`); the per-tenant installed copies of marketplace packs carry a `__<uuid>` identifier suffix but are `custom:false`, so they are not whole-deletable (and must not be — they are the working installs). Not smoke-validated (install is not reversible via the external surface) |
| `soar integration list` / `uninstall` | imperative (modern v1alpha) | ✅ | `list [--custom] [--json]` enumerates installed packs (`soar.ListIntegrations`); `uninstall --name <key>` deletes a **custom** pack (`soar.DeleteIntegration`, guarded). `soar.IsDeletableIntegration` = `custom:true` only — the guardrail that refuses commercial/installed packs. Read live-validated |
| `soar integration connector list` / `delete` | imperative (modern v1alpha) | ✅ | Connector **definitions** (templates inside an integration; distinct from the configured connector *instances* in the reconcile table). `list --integration <key>` (`soar.ListConnectors`, flags `custom`); `delete --integration <key> --id <connId>` (`soar.DeleteConnectorDef` → v1alpha `integrations.connectors.delete`, guarded). Only `custom:true` definitions are deletable (commercial rejected) — the path that removes a duplicated **"Copy of …"** connector without touching the working stock definition. Read + delete live-validated end-to-end |
| `soar settings case-assignment` / `move-case-policy` (`get`/`set`) | imperative | 🔨 | Wave 7: singleton case-routing policies (one record, no id/list/delete) → imperative get/set, not reconcile. `get` is read-only; `set <enum>` is guarded |
| `form-dynamic-parameters` | (deferred) | 🔒 | Wave 7: investigated as a reconcile surface but **not wired** — the strict PUT update silently resets a parameter's `formType` to Invalid (dropping it out of its form) even with the integer-enum body the UI sends, so reconcile update is unsafe. Read-only via `soar legacy call settings/form-dynamic-parameters?formType=CloseCase` |
| `soar legacy call <op>` | raw | ✅ | passthrough for integrations · ontology-mapping (selector read + batch upsert + body delete; the canonical raw-lane case) · environment-priorities · permissions/SSO (read-only by choice) · system/singleton settings · … (batch/bundle/selector) |
| `soar pull/push grouping` | (modern) | 🔨 | pre-existing v1alpha pull + patch — not full reconcile. (`connectors`/`jobs` moved to the reconcile table above on the reliable AppKey path) |

## SIEM · control plane (reconcile)  — auth: ADC/OAuth token

| Surface | Lane | Read | Write | Notes |
|---|---|---|---|---|
| `rules` | bespoke | ✅ | ✅ | YARA-L source + deployment state machine (two resources), not a single canonical body. `push rules-create` · `rules-update` (etag-guarded text update) · `rules-deploy` (reconcile enabled/alerting/frequency) · `rules-disable`. Operational `rules detections/errors/alerts <id>` + `rules retrohunt list/get/create`. Read live-validated; lifecycle write smoke `TestLiveRulesLifecycleWriteSmoke` (create→update→deploy→delete, self-cleaning) |
| `reference_lists` | reconcile | ✅ | ✅ | typed, `.txt`+`.yaml`; NoDelete; engine = product-neutral. Resource-name **normalization**: create echoes the project NUMBER while list echoes the project ID — both are rewritten to the id form so reconcile identity (keyed on the name) stays stable. Read + write live-validated; write smoke `TestLiveReconcileReferenceListWriteSmoke` reuses one fixed inert list (no delete API → can't be a throwaway-and-delete) and drives a fresh create-or-reuse + one update each run (rerunnable, no accumulation) |
| `data_tables` | reconcile | ✅ | ✅ | `.csv`+`.yaml` on the engine; `push data_tables` (create/update). Columns immutable after create; rows are wholesale destroy-and-replace (`ReplaceDataTableRows`). Not prune-eligible (whole-table delete is high-blast). Write smoke `TestLiveReconcileDataTableWriteSmoke` passed (create→update desc→replace rows→delete) |
| `feeds` | reconcile | ✅ | ✅ | `.yaml` on the engine; `push feeds`. Secrets redacted on pull, overlaid on update (real secret preserved; create refuses a masked body); `details` replaced wholesale on PATCH. Fixes: `assetNamespace`(read) vs `namespace`(write) mismatch **resolved** (API uses `assetNamespace`); short `logType` now **expanded to the full resource name** on write (the API rejects a bare id), so feeds round-trip. Server keys stripped; feed state is a runtime toggle, out of canonical. Not prune-eligible (delete stops ingestion). **Read + write live-validated** (created live feeds incl. GCS V2 — `GOOGLE_CLOUD_STORAGE_V2`/`gcsV2Settings`, STS-backed) + gated CUD write-smoke `TestLiveReconcileFeedWriteSmoke` (inert HTTP throwaway, create→update→delete). `FetchFeedServiceAccount` (`feedServiceAccounts:fetchServiceAccountForCustomer`) added for the STS SA grant |
| `parsers` | reconcile | ✅ | ✅ | `.conf`+`.yaml` on the engine; `push parsers`. Versioned/immutable → no server-side update: an edit is **create-new-version + activate** (parser id is volatile, written back on refresh); old version left inactive (rollback). Live set derived from feeds; not prune-eligible. Read + write live-validated; write smoke `TestLiveReconcileParserWriteSmoke` runs `RunParser` (pure inert validation, no server state) then creates a new **INACTIVE** version from a real active parser's source, asserts it never goes ACTIVE (live ingestion untouched), and deletes it. `RunParser` response shape fixed (`parsedEvents` is `{events:[…]}`, not a bare array) |
| `dashboards` | reconcile | ✅ | ✅ | native dashboards (**CUSTOM only**; CURATED read-only/unmanaged) on the engine; `pull`/`push dashboards`. One `<slug>.json` (config + `_server` id), charts inline under `definition.charts` (replaced wholesale on update); `access` immutable after create. extraStrip drops `createUserId`/`updateUserId`/`dashboardUserData`; root `name` stripped (identity in ServerID). `pull` re-pointed from the export-envelope to the config shape. Read live-validated; write smoke (create→update→delete, closure-direct to avoid the heavy full-view list rate-limiting) |
| `curated` / `curated_rules` | imperative (read+toggle) | ✅ | ✅ | Google-managed (no CUD) → `curated list` + guarded `curated set` toggling `enabled`/`alerting` per (category, rule set, precision). New `chronicle/curated_write.go` (single PATCH + updateMask). Imperative lane, not reconcile (fixed catalog, array batch body). Live-validated via a guarded enable→disable toggle that restores prior state. Rule **exclusions** are the separate `rule_exclusions` surface below |
| `rule_exclusions` | reconcile | ✅ | ✅ | findings refinements (display_name + type + UDM query) on the engine; `pull`/`push rule_exclusions`. Create + Update (PATCH, updateMask); **NoDelete** (no delete API → drift reported, never pruned), NoEtag. Deployment toggle (enabled/archived) out of the diff basis. Read + write live-validated (create→update→archive); the API has no hard delete — **archive** (deployment `archived=true`) is the teardown, the state several live exclusions already sit in |
| `watchlists`, `forwarders`, `log_pipelines` | reconcile | — | 📐 | SDK present; wire where per-object CUD fits |

## SIEM · operational plane (query → act)

| Domain | Query | Act | Status | Notes |
|---|---|---|---|---|
| **events (UDM)** | `query udm` · `search nl` · `stats` | — | 🔨 query udm · 📐 rest | immutable telemetry — **read-only** |
| **alerts** | `alerts list/get` | `alerts update/bulk` (verdict/priority/status/comment) | 📐 | standalone Chronicle alerts SDK built (`GetAlerts`/`UpdateAlert`/`BulkUpdateAlerts`). In practice operators read alerts as a **field of the case** via the reliable SOAR AppKey lane (`GetCaseFullDetails.alerts`). Live-test focus in Wave 13 |
| **cases** (Chronicle UUID API) | `cases list/search/get` | (planned) | 🔨 read · ⛔ API | the **same case** as `soar case` (above) reached via the newer Chronicle API (UUID); it returns intermittent 5xx/404, so it is **not used**. One case, two APIs — all case work runs on the reliable SOAR AppKey lane (`soar case`), linked by `soarPlatformInfo.caseId`. Not a separate case system. See SOAR-DESIGN |
| **entities / IoCs** | `entity summarize` · `iocs list` | — | 📐 | enrichment — read-only |

## How to keep this current

When a surface advances: edit its row here **and** the relevant design doc in the
**same commit**. A surface reaches `✅` only after a live read round-trips clean and
(for writes) a gated smoke passed on an inert throwaway — see the build discipline
in [ARCHITECTURE.md](ARCHITECTURE.md) §5. A `⛔` row records *what* is blocked and
*why* (which API, which error) so it can be retried, not silently forgotten.
