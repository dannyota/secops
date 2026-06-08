# Surface map

The authoritative map of **which API does what**, split by plane: every API
family has exactly one home (plane + host + auth + version) and one lane.
[architecture.md](architecture.md) explains the model; [catalog.md](catalog.md)
tracks per-command build status; this file is the inventory and the SIEM-vs-SOAR
split.

## Planes

A **plane** is a `(host, auth)` pair — the *product + transport* axis (orthogonal
to the control-vs-operational planes in [architecture.md](architecture.md) §1,
the *config-vs-data* axis; a surface has both — SIEM reference lists are
SIEM-plane **and** control-plane). SecOps exposes three:

```mermaid
flowchart TB
  cli["secopsctl · soar/ · chronicle/"]

  subgraph SIEM["SIEM plane · chronicle/ · ADC/OAuth"]
    direction TB
    siemH["chronicle.googleapis.com (regional)<br/>v1 › v1beta › v1alpha · per-surface pin"]
  end
  subgraph LEG["SOAR-legacy · soar/legacy/ · AppKey"]
    direction TB
    legH["{tenant}.siemplify-soar.com<br/>/api/external/v1/… · no version ladder"]
  end
  subgraph MOD["SOAR-modern · soar/ · AppKey"]
    direction TB
    modH["{tenant}.siemplify-soar.com<br/>v1alpha only"]
  end

  cli -->|ADC| SIEM
  cli -->|AppKey| LEG
  cli -->|AppKey| MOD
```

| Plane | Host | Auth | Base path | SDK package | Reliability |
|---|---|---|---|---|---|
| **SIEM** | `chronicle.googleapis.com` (regional) | ADC / OAuth | `{v}/projects/{p}/locations/{l}/instances/{i}/…` | `chronicle/` | mostly stable |
| **SOAR — legacy** | `{tenant}.siemplify-soar.com` | AppKey | `/api/external/v1/…` | `soar/legacy/` | **the reliable path** |
| **SOAR — modern** | `{tenant}.siemplify-soar.com` | AppKey | `v1alpha/…/instances/{i}/…` | `soar/` | reads reliable; writes validated per surface |

The chronicle host serves **v1 / v1beta / v1alpha**; we prefer **v1 > v1beta >
v1alpha** and pin the highest version that answers **per surface** (`{v}` above —
e.g. Threat Intel / watchlists / governance = v1, forwarders = v1beta,
curatedRules = v1alpha; full map in [architecture.md](architecture.md) §6). The
SOAR host serves **v1alpha only** (v1/v1beta 404), so the version ladder is a
chronicle-host concern.

Two rules keep the split honest:

1. **A surface is placed by its host+auth, not by how it "feels."** Threat
   Intelligence reads like an external enrichment add-on, but `threatCollections`
   answers on `chronicle.googleapis.com` with ADC, so it is **SIEM-plane**. The
   Content Hub is the mirror-image trap: it uses the modern v1alpha resource shape,
   so it *looks* SIEM — but `marketplaceIntegrations`/`contentPacks` answer on
   `*.siemplify-soar.com` with the AppKey (the chronicle host 500s for them), so
   they are **SOAR-modern plane**. Verify the host before placing.
2. **Some resources exist on two hosts.** For tenants migrated to a customer-managed
   project, `integrations` / `connectors` / `jobs` answer on **both** the SOAR
   AppKey host and `chronicle.googleapis.com` v1alpha. We deliberately operate
   connectors/jobs config-as-code on the **legacy AppKey** lane (reliable) and use
   the modern path only for what the legacy API lacks (e.g. per-connector-definition
   delete). Each such family names the host it uses and why.

Status legend: ✅ built + validated · 🔨 partial / built-not-validated · ⬜ planned (gap) · ⛔ blocked (endpoint flaky/unavailable).

---

## SIEM plane — `chronicle/` (ADC)

### Detection engine

| Family | Lane | Status | Gaps |
|---|---|---|---|
| custom rules (CRUD + revisions) | reconcile | ✅ | — |
| rule deployments (enable/alerting/freq/archived) | reconcile | ✅ | — (`archived` + `ArchiveRule`) |
| rule validation (`verifyRuleText`) + dry-run (`legacyRunTestRule`) | imperative | ✅ `RunTestRule` | live-run validation pending approval |
| detections / errors | operational | ✅ | `legacySearchCuratedDetections` ⬜ (med) |
| retrohunts | imperative | ✅ | — |
| rule exclusions (`findingsRefinements`) | reconcile | ✅ | — |
| curated rule sets / categories / deployments | imperative | ✅ list + per-deployment patch (`curated set`) · `:batchUpdate` (self-restoring toggle write-smoke validated) | single GETs ⬜ (low) |
| curated rules (`curatedRules`) | operational (read) | ✅ list/get (`curated rules`) | — |

### Data, lists & ingestion

| Family | Lane | Status | Gaps |
|---|---|---|---|
| reference lists | reconcile | ✅ | (API has no delete) |
| data tables (+ rows) | reconcile | ✅ | async bulk-row ops, single-row reads ⬜ (low) |
| feeds (+ service account) | reconcile | ✅ | `feedSourceTypeSchemas`/`logTypeSchemas` discovery ✅ read-validated (`chronicle/schemas.go`) · `importPushLogs` ⬜ (low) |
| parsers / parser extensions | reconcile + imperative | ✅ | — |
| log types | read | ✅ list · `GetLogTypeSetting` read-validated · `GetLogType` wired (documented v1alpha) | per-log-type `logTypes.get` is a documented method but 404s "Method not found" on instances that don't enable it (verified across v1/v1beta/v1alpha and both hosts) — enumerate with `ListLogTypes` · `updateLogTypeSetting` ⬜ (med) · event-type suggestions ⬜ (low) |
| forwarders / collectors | reconcile | ✅ **reconcile surface wired + engine write-smoke** (`TestLiveReconcileForwarderWriteSmoke`, create→update→delete); collectors list/get | per-collector CUD ⬜ (collectors are a nested resource) |
| `metricDefinitions` (custom SOC metrics) | reconcile (additive) | 🔨 surface wired + offline-tested; **feature-gated 403 on the tenant** (not enabled/GA), so not live-validated | textDefinition is YARA-L 2.0, immutable; patch is state-only; no delete API |
| `dashboardScheduledReports` | reconcile (full CRUD) | 🔨 surface wired + offline-tested; **reads live-validated** (list 200), create-report backend **500s** server-side | imperative `trigger`/`duplicate`/`fetchHistory` in the SDK; dashboard reduced to a `{name}` ref |
| `dataTaps` (UDM → Pub/Sub) | reconcile | ✅ **write-validated** (`TestLiveReconcileDataTapWriteSmoke`, create→update→delete) | PATCH 501 → update = delete+recreate; supersedes the Backstory endpoint (same chronicle host); needs a Pub/Sub topic + publisher grant for a live tap |
| `errorNotificationConfigs` | reconcile (full CRUD) | 🔨 surface wired + offline-tested; **feature-gated 403** | zero-ingest / size-threshold / normalization-delay → Cloud Monitoring channels |
| `enrichmentControls` | imperative | 🔨 SDK wired + read-attempted; **feature-gated 403** | no patch (records accumulate) + `:disable` verb → imperative, not reconcile |
| `federationGroups` · `tenants` · `multitenantDirectory` (MSSP) | reconcile · read | 🔨 federationGroups reconcile + tenants/directory reads; multitenantDirectory **read-validated**, federationGroups/tenants **403** on a single tenant | multi-tenant only; `chronicle/federation.go` |
| ingestion (`logs`/`events`/`entities:import`) | imperative | ✅ | — |

### Entities, Threat Intel & investigation

| Family | Lane | Status | Gaps |
|---|---|---|---|
| entities (`:summarizeEntity`) | operational | ✅ | `:searchEntities` / `:findEntity*` graph RPCs ⬜ (med) |
| IoC enterprise search (`legacySearchEnterpriseWideIoCs`) | operational | ✅ read-validated (50 matches); association `regionCode` is an object, decoded either way | — |
| **Threat Intelligence** (`threatCollections`) | operational (read) | ✅ list/get (`chronicle/ti.go`, `ti collections`/`collection`) | `:fetchRelated`/`:fetchEntityMetadata`/`:fetchIocMatchMetadata` ⬜ (med) |
| **IoCs** modern (`iocs`) | operational (read) | ✅ `iocs find`/`get` CLI live-validated (`FindIoCs` typed `fieldAndValue` lookup · `GetIoC`/`BatchGetIoCs`) | `fetchRelated`/`getIocState`/`updateIocState` ⬜ |
| `iocAssociations` | operational (read) | ⬜ | get/batchGet/fetchRelated ⬜ (low) |
| UDM search (`:udmSearch`, `:translateUdmQuery`, `:validateQuery`, `:findUdmFieldValues`, NL) | operational | ✅ read-validated; `validateQuery` derives validity from errorType/errorText (no `isValid` field); raw-log search decodes the streamed chunk array (`matches[]`, nested `logType`) | — |
| investigations | operational | ✅ | — |
| alerts (legacy get/fetch/update/bulk) | operational | ✅ `alerts list`/`get` read CLI live-validated; decode tolerant of both legacy-API shapes (array/object, wrapped/bare) | act (update/bulk feedback) built, gated |
| data exports | imperative | ✅ | `:fetchServiceAccountForDataExport` ⬜ (med) |

> **Threat Intelligence is read-only.** `threatCollections` (campaigns / reports /
> actors / malware / vulnerabilities) is Google/Mandiant-sourced — there is no TI
> *write* path. "Applied Threat Intelligence" / "Emerging Threats" detections are
> delivered as **curated rule sets** (above), not a separate API. Custom TI is
> ingested through normal logs + reference lists.

### Admin, governance & Content Hub

| Family | Lane | Status | Gaps |
|---|---|---|---|
| dashboards (native) | reconcile | ✅ | — |
| **data-access labels** (`dataAccessLabels`) | imperative | ✅ SDK CRUD; create→get→delete write-validated (self-cleaning smoke) | imperative, NOT reconcile: create→list lags + create-despite-error break diffing; CLI ⬜ |
| **data-access scopes** (`dataAccessScopes`) | imperative | ✅ SDK CRUD; create→get→delete write-validated (throwaway unassigned scope) | imperative (same quirks as labels); CLI ⬜ |
| **risk config** (`{instance}/riskConfig`) | imperative | ✅ `GetRiskConfig` + idempotent `UpdateRiskConfig` write-validated (singleton sub-resource) | path is the singleton `{instance}/riskConfig` (GET/PATCH), not a colon verb; CLI ⬜ |
| **BigQuery export** (`{instance}/bigQueryExport`) | imperative (read) | ✅ `GetBigQueryExport` wired (pinned **v1**); returns a clean typed error when not provisioned (Enterprise Plus / Pre-GA) | `provision`/`update` ⬜ (gated writes) |
| **entity risk scores** (`entityRiskScores:query`) | operational (read) | ✅ `QueryEntityRiskScores` (filter/orderBy), read-validated (301) | behavioral risk (0–1000); v1alpha |
| **investigations / TIN** (+ `investigationSteps`/`investigationComments`) | operational (read) | ✅ list/get/steps read-validated (250 / steps); `investigationComments` 501 on this tenant; trigger gated | the Gemini Triage & Investigation Agent; `chronicle/investigations.go` + `analytics.go` |
| **coverage details** (`coverageDetails`) | operational (read) | ✅ `ListCoverageDetails` read-validated (MITRE ATT&CK per rule × threat-collection), pinned **v1** | the API view of "emerging threats" coverage |
| Content Hub — featured content rules | read | ✅ | on the chronicle ADC host (distinct from the SOAR-host marketplace below) |
| Content Hub — featured native dashboards | imperative | ⬜ | `list`/`install` ⬜ (med) |
| `instances.get` | read | ⬜ | (low) |

> **The installable Content Hub (`marketplaceIntegrations`, `contentHub/contentPacks`)
> is on the SOAR host, not chronicle.** It answers on `*.siemplify-soar.com` (AppKey)
> using the v1alpha resource shape — the chronicle ADC host returns HTTP 500 for it.
> So it lives on the **SOAR-modern plane** (`soar/marketplace.go`); it is the durable
> twin of the legacy `/store` install path and the only place an integration
> **uninstall** exists. See that table below and the "Other features" rows in
> [catalog.md](catalog.md). The SIEM-host *featured content* rows above are a
> separate, chronicle-side surface. (See the CLAUDE.md rule on host placement.)

---

## SOAR — legacy plane — `soar/legacy/` (AppKey)  ·  the reliable lane

~99.8% of the external API (`third_party/siemplify-swagger.json`) is wrapped: cases
(+ the 9 mutate verbs, bulk), playbooks/workflows, connectors, jobs, environments,
soc-roles, SLA/case-stages/tags/close-root-causes, networks/tracking-lists/block-lists,
ontology, webhooks, store/Content-Hub install, settings singletons, agents, reports,
homepage, permissions, federation, **API-key metadata** (`GetApiKeys` — typed
read-only, no secret; swagger-absent, confirmed live). Lanes: reconcile (per-object
config), raw (batch/bundle/selector via `soar legacy call`), imperative (case verbs,
settings, store).

| Status | Detail |
|---|---|
| ✅ | the whole reliable SOAR surface — see [catalog.md](catalog.md) for per-command rows |
| ⬜ | `getExternalProviders` (SSO provider catalog) — on the non-standard `/api/1p/external/v1/` base the transport can't reach; low priority |

Playbooks/workflows and the SOAR case mutate-verbs exist **only** here — there is no
v1alpha equivalent we rely on.

---

## SOAR — modern plane — `soar/` (AppKey, v1alpha)  ·  preferred per validated surface

Modern v1alpha on the SOAR host, used where it has earned its place. A surface
goes modern-by-default only after it holds up live; until then config-as-code
stays on the legacy lane, which is reliable. So far that means:

- **`soar case list`** is modern-by-default — it calls the v1alpha cases API and
  **auto-falls back to legacy** on error; the global **`--legacy`** flag forces the
  legacy path. The labeled New-vs-Legacy dispatch is the `preferModern` helper.
- **Content Hub** (`marketplaceIntegrations`, `contentPacks`) and **integration /
  connector-definition** delete live here because the legacy API has no equivalent
  (uninstall, per-definition delete).
- The reconcile config surfaces are read on modern where validated; their v1alpha
  **writes are also validated** — create→get→delete on customLists / socRoles /
  caseTagDefinitions, environments create reachable (license-capped) — they do
  **not** 500 (`TestLiveConfigSurfaceWriteSmoke`). The reconcile *engine* still
  runs on the reliable legacy lane; pointing it at the validated modern writes is
  the per-surface flip tracked below. (Per-surface, test before assuming a write
  500s — the official v1alpha REST docs document create/patch/delete for all of
  these, and a past 500 was usually a null-collection or wrong-host shape issue.)

| Family | Status | Gaps (modern) |
|---|---|---|
| integrations catalog (list/get/delete) | ✅ | `updateCustomIntegration`, `:export`/`:download` ⬜ (low) |
| connector **definitions** (list/get/delete) | ✅ | create/patch ⬜ (med) |
| connector **instances** (list/get/patch + `:runOnDemand`) | ✅ list+get read-validated (instance GET decodes the `parameters` descriptor array) | create/delete + `:runOnDemand` SDK-built, live-write pending |
| job **definitions** (list) | 🔨 | get/create/patch/delete ⬜ (med) |
| job **instances** (list/get/patch + `:runOnDemand`) | ✅ list read-validated | create/delete + `:runOnDemand` SDK-built, live-write pending |
| alert grouping rules (list/get/patch + create/delete) | ✅ **full lifecycle write-validated** — create→get→delete on a self-cleaning inert throwaway (`TestLiveAlertGroupingRuleWriteSmoke`); numeric `id` decoded | — |
| module settings | ✅ | — |
| **Content Hub — `marketplaceIntegrations`** | ✅ list/get + install/uninstall (install→uninstall round-trip live-validated, self-cleaning) | `soar/marketplace.go` — the durable twin of legacy `/store`, the only place uninstall exists; modern path is cleanly reversible |
| **Content Hub — `contentHub/contentPacks`** | ✅ list (reads validated) | add/delete/deploy ⬜ |
| cases (list) | ✅ **modern by default** (`soar case list`, auto-fallback to legacy; `--legacy` forces legacy); `ListCasesOpts` sends server-side `filter`/`orderBy`/`expand` (web-UI params) | verbs/writes stay legacy until modern verbs pass a write-smoke |
| environments · socRoles · customLists · caseStage/Close/Tag definitions | ✅ modern read + **write** coverage (`soar/config_surfaces.go`): create/get/update/delete wired; create→get→delete **live-validated** for customLists/socRoles/caseTagDefinitions, environments create reachable (license-capped) — **v1alpha writes do not 500** (`TestLiveConfigSurfaceWriteSmoke`) | reconcile lane still runs on legacy (works); flipping it to the validated modern writes is per-surface |
| SLA · networks · block-lists · ontology · remote agents | ✅ `slaDefinitions` + `soarNetworks` **write-validated** (create→get→delete, `TestLiveDataSurfaceWriteSmoke`) · 🔨 `entitiesBlocklists` read + write-endpoint reachable (HTTP 400, **not** 500) but `action`(`ActionScope`)/`entityType` are undocumented server enums · `ontologyRecords` (2.3 MB) + `remoteAgents` read-confirmed. CRUD wired in `soar/data_surfaces.go` (+`export`/`import`, `deleteAll`, ontology `visualFamilies`/`mappingRules`) | reconcile/raw still on legacy; flip per surface. `slaDefinitions` uses **string** enums (not the legacy ints) |
| `caseQueueFilters` | 🔨 read-confirmed live (siemplify-soar v1alpha) | the case-queue resource is `caseQueueFilters`, not `caseQueueDefinitions` (which 404s); SDK not built |
| cases — **modern verbs** (`patch`/`merge`/`addTag`/`executeBulk*`/`pauseSla`…) | 🔨 documented on the `cases` collection; not yet write-validated | the 9 legacy mutate-verbs are the proven path; modern verbs need a gated write-smoke before flipping |
| case data — `customFields` · `calculatedFieldDefinitions` · `propertySchemaDefinitions` | ✅ **full CRUD write-validated** (create→get→delete, `TestLiveCaseDataWriteSmoke`); `soar/case_data_surfaces.go` | customFields `scopes`="Case"/"Alert" (FREE_TEXT needs no options; "All" 500s); calc `SET_VALUE`/`TEXT`/`targetField=CaseCustom.<field>`/`formulaExpression="…"` and depends on a Free-Text custom field |
| `legacySoarIdpMappingGroups` (IdP → roles/perms/envs) | ✅ **read-validated** (3 groups + external providers); full CRUD SDK in `soar/idp_mappings.go` | **two-host surface** — the docs file it under the chronicle instance path, but it **500s on chronicle**, answers on the SOAR host (AppKey). Writes touch live access |
| data-access (scopes/labels) · **federationGroups** | — | **SIEM-plane** (chronicle ADC host): data-access 404s on SOAR; `federationGroups` carries no "migrated-SOAR" marker and 404s on the SOAR host → it lives on chronicle/ADC, see SIEM table |

---

## How a family earns a home

When adding any surface, fill one **registry entry** in
`internal/mirror/surface_families.go` (see [architecture.md](architecture.md) §7)
before writing code:

```go
SurfaceFamily{ Name, Area, Plane, Host, Auth, Generation, APIVersion, Lane, Status, SDKLocation }
```

The registry is the single source of truth that the [catalog.md](catalog.md)
status matrix and the [architecture.md](architecture.md) §6 version table derive
from. SIEM `APIVersion` is sourced from
`chronicle.APIVersions` (`chronicle/versions.go`), and a drift-guard test
(`surface_families_test.go`) asserts the registry, that map, and the §6 table all
agree — so the map, the docs, and the code can never silently drift.
