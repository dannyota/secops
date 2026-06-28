# Catalog & status

The source of truth for **what exists and how mature it is** — one status per
surface, updated in the same commit that moves the surface forward. Design in
[architecture.md](architecture.md); the API split by plane and unbuilt gaps in
[surfaces.md](surfaces.md); product specifics in [soar.md](soar.md) / [siem.md](siem.md).

This page is the **status spine** — the compact tables. Per-surface detail (one
entry per function) lives alongside in [catalog-siem.md](catalog-siem.md) and
[catalog-soar.md](catalog-soar.md), linked under each table's *Surface details*.

**Where the code is:** surfaces register in `internal/mirror/registry_{soar,siem}.go`
(playbooks: `soar_playbooks.go`; data_tables: `datatables_surface.go`;
connectors/jobs: `soar_operational_surfaces.go`); the
product-neutral engine is `internal/mirror/reconcile`; write-smokes are gated by
`SECOPS_SOAR_SMOKE`/`_WRITE` (`reconcile_smoke_test.go`) for SOAR and
`SECOPS_SIEM_SMOKE`/`_WRITE` (`reconcile_smoke_siem_test.go`) for SIEM.

**This catalog has a machine-readable spine.** The status matrix below is mirrored
by the surface-family registry in `internal/mirror/surface_families.go` — one
declarative `SurfaceFamily` entry per API family (`Area`, `Plane`, `Host`, `Auth`,
`Generation`, `APIVersion`, `Lane`, `Status`, `SDKLocation`). SIEM versions in the
registry are sourced from `chronicle/versions.go` (the `APIVersions` map, the single
source of truth for Chronicle-host version pins), and a drift-guard test
(`surface_families_test.go`) asserts the registry, the version pins, and
[architecture.md](architecture.md) §6 stay in agreement.

**Status legend**

| | Status | Meaning |
|:-:|---|---|
| 📐 | **designed** | spec'd, code not landed |
| 🔨 | **built** | code lands |
| ✅ | **verified** | reads round-trip clean / a write smoke ran |
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
  [architecture.md](architecture.md) §6).
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

```mermaid
flowchart LR
  subgraph chronicle["chronicle.googleapis.com · ADC/OAuth"]
    direction TB
    cnew["New API<br/>v1 &gt; v1beta &gt; v1alpha"]
  end
  subgraph siemplify["*.siemplify-soar.com · AppKey"]
    direction TB
    snew["New API<br/>v1alpha only"]
    sleg["Legacy API<br/>/api/external/v1"]
  end
  cnew --> siem["SIEM families<br/>rules · feeds · parsers · TI"]
  snew --> soarn["SOAR modern<br/>cases-list · grouping · Content Hub"]
  sleg --> soarl["SOAR legacy<br/>reconcile engine · case verbs"]
```

---

## SIEM — Chronicle (`chronicle.googleapis.com`, ADC/OAuth)

Modern-only: the Legacy (Siemplify external) column is `—` throughout — Chronicle has no legacy external API.

### Control plane — config as code (`pull` → `git diff` → `push`)

| Function (CLI) | Lane | Status (marker · domain · version) | Notes |
|---|---|---|---|
| `rules` | bespoke | ✅ chronicle · v1alpha | YARA-L source + deployment state machine; multiple CLI verbs + rule-tuning reads + batch update. `push rules-create` retries deploy with backoff (W118). |
| `reference_lists` | reconcile + imperative | ✅ chronicle · v1alpha | Typed `.txt`+`.yaml`; NoDelete; resource-name normalization; empty-list canonical fix. |
| `data_tables` | reconcile | ✅ chronicle · v1alpha | `.csv`+`.yaml`; columns immutable after create; rows wholesale destroy-and-replace. |
| `feeds` | reconcile | ✅ chronicle · v1alpha | `.yaml`; secrets redacted on pull, overlaid on update; `secret_ref` env/Secret Manager; not prune-eligible. |
| `parsers` | reconcile | ✅ chronicle · v1alpha | Versioned/immutable; edit = create-new-version + activate; parser-dev loop + `validate` verb. |
| `dashboards` | reconcile | ✅ chronicle · v1alpha | Custom dashboards; `create`/`get`/`edit` (metadata + access); `charts` (list/get/add/batch/edit/remove/run — 9 chart types, reserved-word help in `charts add`); `markdown` (add/edit/remove); `button` (add/edit/remove); `layout` (show/move); `filters` (show/set global time range); `lint`/`fix`/`inspect` quality; `verify` (single + fleet, reserved-word error reframe); `duplicate` + deep-copy; export↔import. |
| `curated` / `curated_rules` | reconcile + imperative | ✅ chronicle · v1alpha | Google-managed; batch enable/alerting reconcile; curated tuning reads; v1alpha only. |
| `rule_exclusions` | reconcile + imperative | ✅ chronicle · v1alpha | Findings refinements; NoDelete/NoEtag; guarded deploy toggle; write-validated. |
| `forwarders` | reconcile | ✅ chronicle · v1beta | `.yaml`; prune-eligible; collectors separate nested resource; SDK pinned v1beta (v1 404s). |
| `metric_definitions` | reconcile | 🔨 chronicle · v1alpha | Custom SOC metrics; textDefinition immutable; NoDelete; feature-gated 403 (Pre-GA). |
| `scheduled_reports` | reconcile | 🔨 chronicle · v1alpha | Scheduled dashboard reports; reads verified; create 500s server-side (write-smoke skips). |
| `datataps` | reconcile | ✅ chronicle · v1alpha | Stream UDM events to Pub/Sub; prune-eligible; PATCH 501 so update = delete+create. |
| `error_notifications` | reconcile | 🔨 chronicle · v1alpha | Ingestion-health alerts to Cloud Monitoring channels; feature-gated 403, not verified. |
| `enrichment_controls` | imperative | 🔨 chronicle · v1alpha | Turn off UDM enrichment per log type; no patch, imperative only; feature-gated 403. |
| `federation_groups` · `tenants` (MSSP) | reconcile · read | 🔨 chronicle · v1alpha | Multi-tenant federation; MSSP-only (403 on single tenant); `multitenantDirectory` read-validated. |
| schema discovery | imperative (read) | ✅ chronicle · v1alpha | `feedSourceTypeSchemas`, `logTypeSchemas`, `logTypeSetting`, `logTypes.get`; read-validated. |
| governance — `riskConfig` · `dataAccessLabels`/`Scopes` | imperative | ✅ chronicle · v1 | SDK-only (no CLI yet); write-validated; quirky create (persist-despite-error, list lag, tombstone). |

#### Surface details

Per-surface detail (one entry per function): see [catalog-siem.md](catalog-siem.md).

### Operational plane — query → act (live data)

> **Cases are a SOAR function, surfaced as one command** — the top-level `cases`
> command works the same case on the **siemplify** host, where every verb answers
> (`cases list` prefers v1alpha and auto-falls back to the legacy AppKey queue). The
> same case is also addressable on the **chronicle** host by UUID (ADC), but that
> collection errors at every version, so it is not surfaced — only `cases soar-id`
> (the UUID→id bridge) reads from chronicle. One case, several APIs.

| Function (CLI) | Lane | Status (marker · domain · version) | Notes |
|---|---|---|---|
| **events (UDM)** | operational (read) | 🔨 chronicle · v1alpha · REST | `search udm`, `search stats`; `search run`/`saved` library; NL→UDM is `gemini search` (W75, W81, v0.6.0) |
| **raw logs** | operational (read) | ✅ chronicle · v1alpha | Two paths: `search udm --raw` (UDM-scoped) and `search raw '<regex>'` (content-scoped, reaches unparsed logs) |
| **search output contract** | operational (read) | ✅ chronicle · v1alpha | v0.6.0: `--format jsonl\|json\|csv\|table`, `--fields` dotted UDM-path projection, `--out <file>`, `--all` (complete result set + total match count); `search event <id>`/`export`/`validate` |
| **saved & shared searches** | operational + guarded | ✅ chronicle · v1alpha | v0.6.0: server-side `search saved list/get/run/save/share/unshare/delete`; `save`/`share`/`delete` guarded |
| **alerts** | operational | ✅ read · 🔨 act (CLI wired) | Read + act wired (W52); bulk disposition (W76); alert→case bridge read-validated |
| **entities** | operational (read) | 🔨 chronicle · v1alpha | `entities summarize`/`risk-scores`; enrichment read-only; tolerant `flexInt` for proto3 string-encoded counters |
| **watchlists** (`lists watchlists`) | operational (read) | ✅ chronicle · v1 | `lists watchlists list`/`get`; pinned v1 (`watchlistsAPIVersion`) |
| **analytics & AI reads** | operational (read) | ✅ chronicle (W17, `chronicle/analytics.go`) | Investigations + steps + risk scores + MITRE coverage + BigQuery export; writes gated |
| **alert AI investigation** (`alerts investigate`) | operational | ✅ chronicle · v1alpha | Per-alert Gemini TIN triage (W57); trigger + poll + notebook; `--latest` read-only variant |
| **Gemini NL→UDM** (`gemini generate`/`search`) | operational (read) | ✅ chronicle · v1alpha | v0.6.0: `generate` translates NL→UDM (no run); `search` translates + runs (same output flags); honors the model's suggested time window; one-time `--opt-in`; readonly refuses artifact-creating generations |
| **Gemini assistant** (`gemini ask`) | operational (read) | ✅ chronicle · v1alpha | YARA-L / UDM Q&A; verified (W56); HTML blocks rendered as prose; `--opt-in` |
| **findings graph** (`findingsGraph`) | operational (read) | ✅ chronicle · v1alpha | Graph-pivot from detection id (W56); SDK-only (`chronicle/findings_graph.go`) |
| **alert enrichment** (`alerts enrich`) | operational (read) | ✅ chronicle · v1alpha | Full per-alert detection collection (rule + UDM events + entities + triage) via `legacy:legacyBatchGetCollections` — the surface the console uses. The `enrichmentAgent:*` path (W56) is a dead 500 and unused by the console; the pre-case action verbs that rode it are withheld (the in-case equivalent is `cases run-action`). |
| **watchlist membership** | imperative | 🔨 chronicle (shape validated; op gated per instance) | `lists watchlists add-entity`; UDM Entity envelope required; membership can 501 per instance |

#### Surface details

Per-surface detail (one entry per function): see [catalog-siem.md](catalog-siem.md).

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
so `—` is a routing choice, not an API gap. `soar pull <target> --prune` removes
local files whose live counterpart no longer exists, so the mirror is an exact 1:1
reflection; refused on an incomplete listing to prevent false deletions.

| Function | Lane | Status | Legacy | Notes |
|---|---|---|---|---|
| `webhooks` | reconcile | — | ✅ | Full CUD; create is license-capped. PruneEligible. |
| `environments` | reconcile | ✅ siemplify · v1alpha | 🔒 | Modern write validated; legacy NoDelete (high blast). |
| `networks` | reconcile | ✅ siemplify · v1alpha | ✅ | Write-validated RFC5737 throwaway. PruneEligible. |
| `tracking-lists` | reconcile | — | ✅ | First write-loop proof (clone throwaway). |
| `soc-roles` | reconcile | ✅ siemplify · v1alpha | ✅ | RBAC. Engine-NoDelete; `--prune` never deletes. |
| `idp` | reconcile | — | ✅ | SSO; id-from-body update closure. Engine-NoDelete. |
| `visual-families` | reconcile | — | ✅ | Write smoke; validates `wrapKey` envelope. PruneEligible. |
| `sla-definitions` | reconcile | ✅ siemplify · v1alpha | ✅ | Affects alert routing. Engine-NoDelete. |
| `case-stages` | reconcile | ✅ siemplify · v1alpha | ✅ | Wrapped list. Engine-NoDelete (UI-pollution). |
| `case-tags` | reconcile | ✅ siemplify · v1alpha | 🔨 | Modern create→get→delete verified. |
| `close-root-causes` | reconcile | ✅ siemplify · v1alpha | ✅ | Modern create/delete wired; non-unique names test slug-collision fix. |
| `blacklists` | reconcile | 🔨 siemplify · v1alpha | ✅ | Read + create/get/delete wired; writes reach endpoint (HTTP 400, not 500) but enums undocumented — not write-validated. |
| `playbook-categories` | reconcile | — | ✅ | Write smoke. |
| `playbooks` | reconcile (bespoke) | — | ✅ | uuid rotates → key on name; whole-body save. No New twin. |
| `connectors` | reconcile | — | ✅ | Full CUD on Legacy engine. PruneEligible. |
| `connector-allowlist` | reconcile (derived) | — | ✅ | Alert allow-list view over connector instances. |
| `jobs` | reconcile | — | ✅ | Legacy engine; NoDelete (delete takes a body, not a clean id). |
| `grouping` | reconcile (modern) | ✅ siemplify · v1alpha | — | Alert-grouping rules on v1alpha SOAR host; bespoke Surface. |
| `case-data` | imperative (modern) | ✅ siemplify · v1alpha | — | Wave 16. SDK full CRUD; no CLI wired yet. |

#### Surface details

Per-surface detail (one entry per function): see [catalog-soar.md](catalog-soar.md).

### Operational + imperative — query → act / per-entity verbs

| Function (CLI) | Lane | New API | Legacy | Notes |
|---|---|---|---|---|
| `cases list` / `get <id>` | operational read | ✅ siemplify · v1alpha | ✅ get; list fallback | Cases on v1alpha siemplify domain; auto-falls back to legacy `ListCaseCards`; `--legacy` forces legacy. |
| `cases summarize` / `counts` / `alert recommend` (AI) | operational read | ✅ summarize · ✅ counts · 🔨 recommend create leg — siemplify · v1alpha | — | W56 AI-assist reads; W59 counts; recommend CREATE works, fetch leg not served (400). |
| `cases run-action` | imperative | ✅ siemplify · v1alpha | ✅ | Runs any installed integration action on a case; guarded; dry-run validated. |
| `cases simulation` | imperative | ✅ siemplify · v1alpha | ✅ | Full simulation CRUD + export/import; write round-trip verified. W118 adds `--event-field`/`--alert-field` (FR-39), `export`, `import`. |
| `cases <verb>` (assign/rename/stage/tag/untag/describe/importance/priority/close/reopen/merge + `comment add`) | imperative | — | ✅ 9 verbs · 🔨 W52 (smoke extended, gated) | 9 verbs verified by `TestLiveSOARCaseVerbsWriteSmoke`; W52 adds priority/reopen/comment. |
| `cases alert <verb>` (close/priority/move/reopen) | imperative | — | 🔨 smoke extended, gated | Per-alert triage (W52); dry-run + error paths validated; writes ride extended write smoke. |
| `playbooks generate` (AI drafting) | imperative | 🔨 siemplify · v1alpha (guarded; dry-run validated) | — | W56 Gemini drafting; creates a draft on the instance; guarded. Write smoke gated. |
| `soar playbooks` operational helpers | operational read + guarded execution | — | ✅ | Waves 39 + 51 + 55; lifecycle ops, export, import, deploy toggle, batch delete, step insert, run/debug/rerun. |
| `playbooks get` / `list` (enriched) / `lint` / `health` / `diff` / `duplicate` | operational read + guarded duplicate | — | ✅ | W118; full authoring inspection: get (structure+deps), lint (static analysis), health (fleet stats), diff (live vs local), duplicate. |
| **playbook authoring palette** (`playbooks components`) | operational read | ✅ siemplify · v1alpha wildcard catalogs | — | W58 designer palette as CLI catalogs; all verified. |
| `soar jobs` operational helpers | operational read + guarded execution | — | 🔨 | Waves 39 + 55; instance set/create/delete, job run, logs. |
| `soar push bulk-close` | imperative | — | 🔨 | Queue bulk-close with typed reason enum. |
| `soar settings case-assignment` / `move-case-policy` (`get`/`set`) | imperative | — | 🔨 | Singleton routing policies; guarded `set`. |
| `soar settings grouping` (`get`/`set`) | imperative (modern + legacy) | ✅ read · ✅ write — siemplify · v1alpha + legacy | — | W80/W107; full General/Overflow property bag (Timeframe/overflow/co-grouping) read+write via moduleSettings; legacy max-alerts-per-case singleton is read-only. |
| `soar settings api-keys` (list/create/revoke) | operational | — | ✅ | W60; key lifecycle verified; key value shown once, never logged. |
| `idp-mappings` | imperative | ✅ siemplify · v1alpha | — | SDK-only (no CLI). 500s on chronicle; answers on SOAR host. Read-validated. |
| `form-dynamic-parameters` | deferred | 🔒 siemplify · v1alpha (unsafe PUT) | 🔒 read | Not wired; PUT silently resets `formType` to Invalid. |
| `soar legacy call <op>` | raw | — | ✅ | Passthrough for integrations, ontology, settings, environment-priorities, permissions. |

#### Surface details

Per-surface detail (one entry per function): see [catalog-soar.md](catalog-soar.md).

## Other features — cross-cutting (domain varies per row)

Grouped by feature, not by domain. The New-API cell names the domain because these span both Google (chronicle) and Siemplify (siemplify).

### Threat Intelligence (Mandiant / Emerging Threats)

| Function (CLI) | Lane | New API (status · domain · version) | Legacy | Notes |
|---|---|---|---|---|
| `ti collections` / `collection <id>` / `collection-matches` / `related` | operational read | ✅ chronicle · v1 | — | Mandiant `threatCollections`; list/get read-validated; related pivots built + offline-tested; pinned v1; uses project number. |
| IoCs — `ti find` / `ti get` / `ti related` | operational read | ✅ chronicle · v1 | — | Modern IoC lookup, read-validated; type auto-detected; SDK in `chronicle/ti.go`; related pivots built + offline-tested. |

#### Surface details

Per-surface detail (one entry per function): see [catalog-soar.md](catalog-soar.md).

### Content Hub & integrations

Installing content (integration packages and the connector/job/action definitions they carry) and the marketplace catalog. Configured integration instances are environment-scoped and operated imperatively. All on the siemplify domain.

| Function (CLI) | Lane | New API (status · domain · version) | Legacy (siemplify · external) | Notes |
|---|---|---|---|---|
| `content-hub list` / `get` / `contentpacks` / `browse` | imperative read | ✅ siemplify · v1alpha | — | Content Hub reads; 405 integrations + 59 packs; read-validated; install round-trip verified (W11). |
| `content-hub install` / `uninstall` | imperative | ✅ siemplify · v1alpha | 🔨 (`/store`) | Reversible marketplace install pair (`marketplaceIntegrations:install`/`:uninstall`); guarded; install→uninstall round-trip verified (W11, W110). |
| `info soar-integrations` | operational read | 🔨 siemplify · v1alpha | 🔨 | Coverage report joining packs with runtime cards; flags config/runtime gaps; built + offline-tested. |
| `info cron` | offline utility | — | — | Scans local scheduler files + pulled SOAR JSON for scheduled automation; no API call. |
| `soar ide build-playbook` / `playbooks mold` / `playbooks trigger set` | offline utility | — | — | Composes save-ready playbook JSON from an exported base; offline-only; built + offline-tested. |
| `integrations scaffold` / `soar ide package-integration` | offline utility | — | — | Scaffolds Python custom integration dirs; packages to ZIP for IDE import; no API call. |
| `commands` / `status surfaces` / `status capabilities` | offline utility + live probe | — | — | Machine-readable registries; W73 rich flag schema + `status capabilities` live probe; verified. |
| `skill` / `skill install` | offline utility | — | — | W84; the agent operating guide embedded in the binary (`go install` ships no docs); `--json` for metadata; `install` registers it in an agent skills dir. |
| structured `--json` errors + dry-run plan | output contract | — | — | W73 verified; stderr structured error envelope + stdout dry-run change plan. |
| `integrations list` / `uninstall` | imperative | ✅ siemplify · v1alpha | — | Lists installed packs; uninstalls custom-only (`custom:true`); read verified. |
| `integrations connector list` / `delete` | imperative | ✅ siemplify · v1alpha | — | Connector definitions inside a pack; delete custom-only; read + delete verified. |
| `integrations get` | operational read | ✅ siemplify · v1alpha + legacy | — | W118; rich detail: version, instances across envs, playbook usage. |
| `integrations test` | operational read | ✅ siemplify · legacy | — | W118; connectivity test for an integration instance (default or `--instance`). |
| `integrations create` / `instances` / `configure` / `delete` (instances) | imperative | — | 🔨 | Integration instances; not reconcilable; CRUD with auto-resolve + configure overlay; `TestLiveIntegrationInstanceCRUD` validated. |
| `integrations install` (+ pack `:install`/`:uninstall`) | imperative/raw | ✅ siemplify · v1alpha (`:install`/`:uninstall`) | 🔨 (`/store`) | Installs marketplace pack via v1alpha; verified install→uninstall round-trip (W11). |
| `integrations action` / `job-def` (template/create/update/delete) | imperative | ✅ siemplify · v1alpha | 🔨 | Python definition authoring loop (W60+W65); `TestLiveAuthoringWriteSmoke` validated for actions. |

#### Surface details

Per-surface detail (one entry per function): see [catalog-soar.md](catalog-soar.md).

## How to keep this current

When a surface advances: edit its row here **and** the relevant design doc in the
**same commit**. A surface reaches `✅` only after a read round-trips clean and
(for writes) a gated smoke passed on an inert throwaway — see the build discipline
in [architecture.md](architecture.md) §5.

A `⛔` belongs to a **specific column + domain + version** that's down — never to a
whole function. If the function works on *any* path (another domain or version),
its row stays green and the dead path is a **note** (as with cases: ✅ on siemplify
v1alpha; the chronicle-host UUID path 500s, noted, not blocking). When the working
version of a Chronicle-host New-API surface moves, change the pin in
`chronicle/versions.go` (the `APIVersions` map), then update the cell's
`domain · version` here and the §6 table in [architecture.md](architecture.md). The
surface-family registry (`internal/mirror/surface_families.go`) reads its SIEM
versions from that map, and the drift-guard test fails if the three fall out of sync.
