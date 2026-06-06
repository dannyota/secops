# Catalog & status

The source of truth for **what exists and how mature it is**. Every surface
carries a status; update it in the same commit that moves the surface forward.
Design in [ARCHITECTURE.md](ARCHITECTURE.md); product specifics in
[SOAR-DESIGN.md](SOAR-DESIGN.md) / [SIEM-DESIGN.md](SIEM-DESIGN.md).

**Where the code is:** surfaces register in `internal/mirror/registry_{soar,siem}.go`
(playbooks: `soar_playbooks.go`; data_tables: `datatables_surface.go`); the
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
| `networks` | reconcile | `id` | ✅ | ✅ | write smoke (RFC5737 throwaway) |
| `tracking-lists` | reconcile | `id` | ✅ | ✅ | first write-loop proof (clone throwaway) |
| `soc-roles` | reconcile | `id` | ✅ | 🔒 | RBAC — read-only |
| `idp` | reconcile | `id`(uuid) | ✅ | 🔒 | SSO; id-from-body update closure — read-only |
| `visual-families` | reconcile | `id` | ✅ | ✅ | write smoke; validates the `wrapKey` envelope |
| `sla-definitions` | reconcile | `id` | ✅ | 🔒 | affects alert routing — read-only |
| `case-stages` | reconcile | `id` | ✅ | 🔒 | wrapped list; UI-pollution — read-only |
| `case-tags` | reconcile | `id` | ✅ | 🔨 | wrapped list; write smoke skips (no tag to clone) |
| `close-root-causes` | reconcile | `id` | ✅ | ✅ | non-unique names → exercises the slug-collision fix |
| `blacklists` | reconcile | `id` | ✅ | ✅ | model block-list; write smoke |
| `playbook-categories` | reconcile | `id` | ✅ | ✅ | write smoke |
| `playbooks` | reconcile (bespoke) | **name** | ✅ | ✅ | uuid rotates → key on name; whole-body save; SavePlaybook update-by-name **verified** |

## SOAR · imperative + raw

| Surface | Lane | Status | Notes |
|---|---|---|---|
| `soar case list` / `get <id>` | operational read | ✅ | **the reliable operational read path** (AppKey), live read-validated. `list` (`ListCaseCards`; `--status`/`--limit`/`--json`) + `get <id>` (`GetCaseFullDetails` → case **and its alerts**, each with its `--alert` identifier for the mutate verbs); table or `--json` |
| `soar case <verb>` (assign/rename/stage/tag/untag/describe/importance/close/merge) | imperative | 🔨 | **the reliable operational case path** (AppKey). 9 mutate verbs; swagger-verified bodies + unit test; dry-run validated, live mutation not exercised |
| `soar push bulk-close` | imperative | 🔨 | queue bulk-close (`ExecuteBulkCloseCase`, AppKey) — pre-existing |
| `soar legacy call <op>` | raw | ✅ | passthrough for integrations · jobs · ontology-mapping · permissions · settings · … (batch/bundle/selector) |
| `soar pull/push connectors\|jobs\|grouping` | (modern) | 🔨 | pre-existing v1alpha pull + patch — not full reconcile |

## SIEM · control plane (reconcile)  — auth: ADC/OAuth token

| Surface | Lane | Read | Write | Notes |
|---|---|---|---|---|
| `rules` | bespoke | ✅ | ✅ | YARA-L source + deployment state machine (two resources), not a single canonical body. `push rules-create` · `rules-update` (etag-guarded text update) · `rules-deploy` (reconcile enabled/alerting/frequency) · `rules-disable`. Operational `rules detections/errors/alerts <id>` + `rules retrohunt list/get/create`. Read live-validated; lifecycle write smoke `TestLiveRulesLifecycleWriteSmoke` (create→update→deploy→delete, self-cleaning) |
| `reference_lists` | reconcile | ✅ | 🔨 | typed, `.txt`+`.yaml`; NoDelete; engine = product-neutral |
| `data_tables` | reconcile | ✅ | ✅ | `.csv`+`.yaml` on the engine; `push data_tables` (create/update). Columns immutable after create; rows are wholesale destroy-and-replace (`ReplaceDataTableRows`). Not prune-eligible (whole-table delete is high-blast). Write smoke `TestLiveReconcileDataTableWriteSmoke` passed (create→update desc→replace rows→delete) |
| `feeds` | reconcile | ✅ | 🔨 | `.yaml` on the engine; `push feeds`. Secrets redacted on pull, overlaid on update (real secret preserved; create refuses a masked body); `details` replaced wholesale on PATCH. The `assetNamespace`(read) vs `namespace`(write) mismatch is **resolved** — the API uses `assetNamespace` (write side fixed); server keys (`lastV2MigrationAttemptTime`/`stsMigrationReadiness`) stripped; feed state is a runtime toggle, out of canonical. Not prune-eligible (delete stops ingestion). Read live-validated; write smoke gated |
| `parsers` | reconcile | ✅ | 🔨 | `.conf`+`.yaml` on the engine; `push parsers`. Versioned/immutable → no server-side update: an edit is **create-new-version + activate** (parser id is volatile, written back on refresh); old version left inactive (rollback). Live set derived from feeds; not prune-eligible. Read live-validated; write smoke gated |
| `dashboards` | reconcile | ✅ | ✅ | native dashboards (**CUSTOM only**; CURATED read-only/unmanaged) on the engine; `pull`/`push dashboards`. One `<slug>.json` (config + `_server` id), charts inline under `definition.charts` (replaced wholesale on update); `access` immutable after create. extraStrip drops `createUserId`/`updateUserId`/`dashboardUserData`; root `name` stripped (identity in ServerID). `pull` re-pointed from the export-envelope to the config shape. Read live-validated; write smoke (create→update→delete, closure-direct to avoid the heavy full-view list rate-limiting) |
| `curated` / `curated_rules` | imperative (read+toggle) | ✅ | ✅ | Google-managed (no CUD) → `curated list` + guarded `curated set` toggling `enabled`/`alerting` per (category, rule set, precision). New `chronicle/curated_write.go` (single PATCH + updateMask). Imperative lane, not reconcile (fixed catalog, array batch body). Live-validated via a guarded enable→disable toggle that restores prior state. Rule **exclusions** are the separate `rule_exclusions` surface below |
| `rule_exclusions` | reconcile | ✅ | ✅ | findings refinements (display_name + type + UDM query) on the engine; `pull`/`push rule_exclusions`. Create + Update (PATCH, updateMask); **NoDelete** (no delete API → drift reported, never pruned), NoEtag. Deployment toggle (enabled/archived) out of the diff basis. Read + write live-validated (create→update→archive); the API has no hard delete — **archive** (deployment `archived=true`) is the teardown, the state several live exclusions already sit in |
| `watchlists`, `forwarders`, `log_pipelines` | reconcile | — | 📐 | SDK present; wire where per-object CUD fits |

## SIEM · operational plane (query → act)

| Domain | Query | Act | Status | Notes |
|---|---|---|---|---|
| **events (UDM)** | `query udm` · `search nl` · `stats` | — | 🔨 query udm · 📐 rest | immutable telemetry — **read-only** |
| **alerts** | `alerts list/get` | `alerts update/bulk` (verdict/priority/status/comment) | 📐 | standalone Chronicle alerts SDK built (`GetAlerts`/`UpdateAlert`/`BulkUpdateAlerts`). In practice operators read alerts as a **field of the case** via the reliable SOAR AppKey lane (`GetCaseFullDetails.alerts`). Live-test focus in Wave 8 |
| **cases** (Chronicle UUID API) | `cases list/search/get` | (planned) | 🔨 read · ⛔ API | the **same case** as `soar case` (above) reached via the newer Chronicle API (UUID); it returns intermittent 5xx/404, so it is **not used**. One case, two APIs — all case work runs on the reliable SOAR AppKey lane (`soar case`), linked by `soarPlatformInfo.caseId`. Not a separate case system. See SOAR-DESIGN |
| **entities / IoCs** | `entity summarize` · `iocs list` | — | 📐 | enrichment — read-only |

## How to keep this current

When a surface advances: edit its row here **and** the relevant design doc in the
**same commit**. A surface reaches `✅` only after a live read round-trips clean and
(for writes) a gated smoke passed on an inert throwaway — see the build discipline
in [ARCHITECTURE.md](ARCHITECTURE.md) §5. A `⛔` row records *what* is blocked and
*why* (which API, which error) so it can be retried, not silently forgotten.
