# Surface map

The authoritative map of **which API does what**, split by plane. This is the
control layer for the SDK: every API family has exactly one home (plane + host +
auth + version) and one lane. [ARCHITECTURE.md](ARCHITECTURE.md) explains the
model; [CATALOG.md](CATALOG.md) tracks per-command build status; this file is the
inventory and the SIEM-vs-SOAR split.

## Planes

A **plane** here is a `(host, auth)` pair — the *product + transport* axis. (This
is orthogonal to the control-vs-operational planes in [ARCHITECTURE.md](ARCHITECTURE.md)
§1, which is the *config-vs-data* axis. A surface has both: e.g. SIEM reference
lists are **SIEM-plane** and **control-plane**.) SecOps exposes three:

| Plane | Host | Auth | Base path | SDK package | Reliability |
|---|---|---|---|---|---|
| **SIEM** | `chronicle.googleapis.com` (regional) | ADC / OAuth | `{v}/projects/{p}/locations/{l}/instances/{i}/…` | `chronicle/` | mostly stable |
| **SOAR — legacy** | `{tenant}.siemplify-soar.com` | AppKey | `/api/external/v1/…` | `soar/legacy/` | **the reliable path** |
| **SOAR — modern** | `{tenant}.siemplify-soar.com` | AppKey | `v1alpha/…/instances/{i}/…` | `soar/` | intermittent 500s |

The chronicle host serves **v1 / v1beta / v1alpha**; we prefer **v1 > v1beta >
v1alpha** and pin the highest version that answers **per surface** (`{v}` above —
e.g. Threat Intel / watchlists / governance = v1, forwarders = v1beta,
curatedRules = v1alpha; full map in [ARCHITECTURE.md](ARCHITECTURE.md) §6). The
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
| feeds (+ service account) | reconcile | ✅ | `feedSourceTypeSchemas`/`logTypeSchemas` discovery ⬜ (med) · `importPushLogs` ⬜ (low) |
| parsers / parser extensions | reconcile + imperative | ✅ | — |
| log types | read | 🔨 list | `getLogTypeSetting`/`updateLogTypeSetting` ⬜ (med) · event-type suggestions ⬜ (low) |
| forwarders / collectors | reconcile | ✅ forwarder CRUD write-validated (throwaway create→update→delete); collectors list/get | reconcile-surface wiring ⬜; `feedSourceTypeSchemas`/`logTypeSetting` ⬜ (deferred) |
| ingestion (`logs`/`events`/`entities:import`) | imperative | ✅ | — |

### Entities, Threat Intel & investigation

| Family | Lane | Status | Gaps |
|---|---|---|---|
| entities (`:summarizeEntity`) | operational | ✅ | `:searchEntities` / `:findEntity*` graph RPCs ⬜ (med) |
| IoC enterprise search (`legacySearchEnterpriseWideIoCs`) | operational | ✅ | — |
| **Threat Intelligence** (`threatCollections`) | operational (read) | ✅ list/get (`chronicle/ti.go`, `ti collections`/`collection`) | `:fetchRelated`/`:fetchEntityMetadata`/`:fetchIocMatchMetadata` ⬜ (med) |
| **IoCs** modern (`iocs`) | operational (read) | ✅ `iocs find`/`get` CLI live-validated (`FindIoCs` typed `fieldAndValue` lookup · `GetIoC`/`BatchGetIoCs`) | `fetchRelated`/`getIocState`/`updateIocState` ⬜ |
| `iocAssociations` | operational (read) | ⬜ | get/batchGet/fetchRelated ⬜ (low) |
| UDM search (`:udmSearch`, `:translateUdmQuery`, `:validateQuery`, `:findUdmFieldValues`, NL) | operational | ✅ | — |
| investigations | operational | ✅ | — |
| alerts (legacy get/fetch/update/bulk) | operational | ✅ | — |
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
| **data-access scopes** (`dataAccessScopes`) | imperative | 🔨 SDK CRUD; list read-validated | write validation ⬜ (binds labels to a role — higher blast); CLI ⬜ |
| **risk config** (`{instance}/riskConfig`) | imperative | ✅ `GetRiskConfig` live-validated (singleton sub-resource, returns defaults); `UpdateRiskConfig` built | path is the singleton `{instance}/riskConfig` (GET/PATCH), not a colon verb |
| BigQuery export config | imperative | ⬜ | get/update ⬜ (low) |
| Content Hub — featured content rules | read | ✅ | on the chronicle ADC host (distinct from the SOAR-host marketplace below) |
| Content Hub — featured native dashboards | imperative | ⬜ | `list`/`install` ⬜ (med) |
| `instances.get` | read | ⬜ | (low) |

> **The installable Content Hub (`marketplaceIntegrations`, `contentHub/contentPacks`)
> is on the SOAR host, not chronicle.** It answers on `*.siemplify-soar.com` (AppKey)
> using the v1alpha resource shape — the chronicle ADC host returns HTTP 500 for it.
> So it lives on the **SOAR-modern plane** (`soar/marketplace.go`); it is the durable
> twin of the legacy `/store` install path and the only place an integration
> **uninstall** exists. See that table below and the "Other features" rows in
> [CATALOG.md](CATALOG.md). The SIEM-host *featured content* rows above are a
> separate, chronicle-side surface. (See the CLAUDE.md rule on host placement.)

---

## SOAR — legacy plane — `soar/legacy/` (AppKey)  ·  the reliable lane

~99.8% of the external API (`third_party/siemplify-swagger.json`) is wrapped: cases
(+ the 9 mutate verbs, bulk), playbooks/workflows, connectors, jobs, environments,
soc-roles, SLA/case-stages/tags/close-root-causes, networks/tracking-lists/block-lists,
ontology, webhooks, store/Content-Hub install, settings singletons, agents, reports,
homepage, permissions, federation. Lanes: reconcile (per-object config), raw (batch/
bundle/selector via `soar legacy call`), imperative (case verbs, settings, store).

| Status | Detail |
|---|---|
| ✅ | the whole reliable SOAR surface — see [CATALOG.md](CATALOG.md) for per-command rows |
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
- Everything else (the reconcile config surfaces) is read on modern where validated
  but **written on legacy** — the v1alpha write endpoints 500 intermittently.

| Family | Status | Gaps (modern) |
|---|---|---|
| integrations catalog (list/get/delete) | ✅ | `updateCustomIntegration`, `:export`/`:download` ⬜ (low) |
| connector **definitions** (list/get/delete) | ✅ | create/patch ⬜ (med) |
| connector **instances** (list/get/patch + `:runOnDemand`) | 🔨 | create/delete ⬜ |
| job **definitions** (list) | 🔨 | get/create/patch/delete ⬜ (med) |
| job **instances** (list/get/patch + `:runOnDemand`) | 🔨 | create/delete ⬜ |
| alert grouping rules (list/get/patch + create/delete) | ✅ SDK lifecycle; list read-validated | live-write pending |
| module settings | ✅ | — |
| **Content Hub — `marketplaceIntegrations`** | ✅ list/get + install/uninstall (reads validated) | `soar/marketplace.go` — the durable twin of legacy `/store`, the only place uninstall exists |
| **Content Hub — `contentHub/contentPacks`** | ✅ list (reads validated) | add/delete/deploy ⬜ |
| cases (list) | ✅ **modern by default** (`soar case list`, auto-fallback to legacy; `--legacy` forces legacy) | verbs/writes stay legacy until modern verbs pass a write-smoke; `--status` filtered client-side on modern |
| environments · socRoles · customLists · caseStage/Close/Tag/QueueDefinitions | ✅ modern read coverage (`soar/config_surfaces.go`, live-validated) | reconcile lane still runs on legacy (works); re-pointing it to v1alpha-with-fallback is optional/per-surface |
| data-access (scopes/labels) | — | 404 on SOAR host — these are SIEM-plane (chronicle ADC host), see SIEM table |

---

## How a family earns a home

When adding any surface, fill one **registry entry** in
`internal/mirror/surface_families.go` (see ARCHITECTURE §7) before writing code:

```
SurfaceFamily{ Name, Area, Plane, Host, Auth, Generation, APIVersion, Lane, Status, SDKLocation }
```

The registry is the single source of truth that the CATALOG status matrix and the
ARCHITECTURE §6 version table derive from. SIEM `APIVersion` is sourced from
`chronicle.APIVersions` (`chronicle/versions.go`), and a drift-guard test
(`surface_families_test.go`) asserts the registry, that map, and the §6 table all
agree — so the map, the docs, and the code can never silently drift.
