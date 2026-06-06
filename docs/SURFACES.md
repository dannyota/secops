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
| **SIEM** | `chronicle.googleapis.com` (regional) | ADC / OAuth | `v1alpha/projects/{p}/locations/{l}/instances/{i}/…` | `chronicle/` | mostly stable |
| **SOAR — legacy** | `{tenant}.siemplify-soar.com` | AppKey | `/api/external/v1/…` | `soar/legacy/` | **the reliable path** |
| **SOAR — modern** | `{tenant}.siemplify-soar.com` | AppKey | `v1alpha/…/instances/{i}/…` | `soar/` | intermittent 500s |

Two rules keep the split honest:

1. **A surface is placed by its host+auth, not by how it "feels."** The Content
   Hub and `marketplaceIntegrations` install integrations, so they feel SOAR — but
   they answer on `chronicle.googleapis.com` with ADC, so they are **SIEM-plane**.
   Threat Intelligence likewise is SIEM-plane.
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
| rule deployments (enable/alerting/freq) | reconcile | ✅ | deployment `archived` field ⬜ (med) |
| rule validation (`verifyRuleText`) | imperative | ✅ | `legacyRunTestRule` dry-run-against-data ⬜ (high) |
| detections / errors | operational | ✅ | `legacySearchCuratedDetections` ⬜ (med) |
| retrohunts | imperative | ✅ | — |
| rule exclusions (`findingsRefinements`) | reconcile | ✅ | — |
| curated rule sets / categories / deployments | imperative | 🔨 list + per-deployment patch | `…:batchUpdate` deployments ⬜ (high) · `listCuratedRules`/`getCuratedRule` ⬜ (high) · single GETs ⬜ (low) |

### Data, lists & ingestion
| Family | Lane | Status | Gaps |
|---|---|---|---|
| reference lists | reconcile | ✅ | (API has no delete) |
| data tables (+ rows) | reconcile | ✅ | async bulk-row ops, single-row reads ⬜ (low) |
| feeds (+ service account) | reconcile | ✅ | `feedSourceTypeSchemas`/`logTypeSchemas` discovery ⬜ (med) · `importPushLogs` ⬜ (low) |
| parsers / parser extensions | reconcile + imperative | ✅ | — |
| log types | read | 🔨 list | `getLogTypeSetting`/`updateLogTypeSetting` ⬜ (med) · event-type suggestions ⬜ (low) |
| forwarders / collectors | reconcile | ⬜ | full CRUD + collectors ⬜ (med) |
| ingestion (`logs`/`events`/`entities:import`) | imperative | ✅ | — |

### Entities, Threat Intel & investigation
| Family | Lane | Status | Gaps |
|---|---|---|---|
| entities (`:summarizeEntity`) | operational | ✅ | `:searchEntities` / `:findEntity*` graph RPCs ⬜ (med) |
| IoC enterprise search (`legacySearchEnterpriseWideIoCs`) | operational | ✅ | — |
| **Threat Intelligence** (`threatCollections`) | operational (read) | ⬜ | `list`/`get` ⬜ (high) · `fetchRelated`/`fetchEntityMetadata`/`fetchIocMatchMetadata` ⬜ (med) |
| **IoCs** modern (`iocs`) | operational (read) | ⬜ | `:find` ⬜ (high) · `get`/`batchGet`/`fetchRelated` ⬜ (med) |
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
| **data-access labels** (`dataAccessLabels`) | reconcile | ⬜ | full CRUD ⬜ (high) |
| **data-access scopes** (`dataAccessScopes`) | reconcile | ⬜ | full CRUD ⬜ (high) |
| **risk config** (`:getRiskConfig`/`:updateRiskConfig`) | imperative | ⬜ | get/update ⬜ (high) |
| BigQuery export config | imperative | ⬜ | get/update ⬜ (low) |
| Content Hub — featured content rules | read | ✅ | — |
| **Content Hub — `marketplaceIntegrations`** | imperative | ⬜ | `list`/`get`/`install`/`uninstall` ⬜ (high) |
| **Content Hub — `contentHub.contentPacks`** | imperative / raw | ⬜ | list/get/add/delete/deploy ⬜ (med) |
| Content Hub — featured native dashboards | imperative | ⬜ | `list`/`install` ⬜ (med) |
| `instances.get` | read | ⬜ | (low) |

> **`marketplaceIntegrations` is the durable twin of the legacy `/store` install
> path** the SOAR-legacy plane currently uses — and the only place an integration
> **uninstall** exists. It is SIEM-plane (`chronicle.googleapis.com`).

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

## SOAR — modern plane — `soar/` (AppKey, v1alpha)  ·  use only where legacy lacks it

A discovery-and-cleanup surface. Most config-as-code is deliberately served on the
legacy lane because these v1alpha endpoints 500 intermittently; only wire a modern
method when the legacy API has no equivalent (e.g. per-connector-definition delete).

| Family | Status | Gaps (modern) |
|---|---|---|
| integrations catalog (list/get/delete) | ✅ | `updateCustomIntegration`, `:export`/`:download` ⬜ (low) |
| connector **definitions** (list/get/delete) | ✅ | create/patch ⬜ (med) |
| connector **instances** (list/get/patch) | 🔨 | create/delete ⬜ (high) · `:runOnDemand` ⬜ (med) |
| job **definitions** (list) | 🔨 | get/create/patch/delete ⬜ (med) |
| job **instances** (list/get/patch) | 🔨 | create/delete ⬜ (med) · `:runOnDemand` ⬜ (med) |
| alert grouping rules (list/get/patch) | 🔨 | create/delete ⬜ (high) |
| module settings | ✅ | — |
| cases (list) | ⛔ | the rest is served on the legacy lane (v1alpha cases flaky) |
| environments / soc-roles / custom-lists / case-definitions / data-access | ⬜ | served on legacy today; modern CRUD deferred until v1alpha stabilizes |

---

## How a family earns a home

When adding any surface, fill one **registry entry** (see ARCHITECTURE §7) before
writing code:

```
SurfaceFamily{ Name, Plane, Host, Auth, APIVersion, Lane, Status, SDKLocation }
```

The registry is the single source of truth that the CATALOG status matrix and the
ARCHITECTURE §6 version table derive from — so the map, the docs, and the code can
never silently drift.
