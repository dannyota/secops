# secopsctl / Go SDK — Roadmap

`secopsctl` is both a CLI and an importable, unofficial Go SDK for Google SecOps
(`danny.vn/secops/chronicle`). This roadmap is the **forward plan and
sequencing**; **live build/validation status lives in [CATALOG.md](CATALOG.md)** —
this doc deliberately does not re-track maturity (it would drift). The guiding
rule: **design cleanly, port the parity slice first, then finish the surface** —
improving on the official Python wrapper where it is weak (see the `// DEVIATION:`
markers in code).

## Package map

```
danny.vn/secops
├── auth/         split credentials: OAuth/ADC (SIEM) + API key/AppKey (SOAR, key-auth)
├── chronicle/    the SIEM SDK (pure API, typed structs, no file I/O)
├── config/       instance config (YAML) load/validate/defaults
├── internal/
│   ├── cli/      cobra command tree (secopsctl)
│   └── mirror/   pull/push file mirroring on top of chronicle
└── cmd/secopsctl main
```

Future SecOps products are **sibling packages** so `chronicle` stays focused —
today that is `danny.vn/secops/soar`. (Third-party EDR and chat/notification
integrations are explicit non-goals; see below.)

---

## Wave 1 — parity with the legacy Python tool *(shipped — status in CATALOG)*

Feature parity with the original `secopstips`:

| Area | SDK (`chronicle/`) | Mirror / CLI |
|---|---|---|
| Rules | `rules.go` — List/Get/Validate/Create, deployments | `pull_rules`, `push` (create + disable) |
| Reference lists | `reflists.go` | `pull_reflists` |
| Data tables | `datatables.go` | `pull_datatables` (CSV) |
| Dashboards | `dashboards.go` | `pull_dashboards` (export CUSTOM) |
| Curated | `curated.go` | `pull_curated` + `pull_curated_rules` |
| Feeds | `feeds.go` | `pull_feeds` (secret redaction) |
| Parsers | `parsers.go` | `pull_parsers` (active CBN) |
| UDM search | `search.go` | `query udm` |

CLI: `info`, `pull <target>`, `push <target>` (dry-run-guarded), `query udm`.

### Deviations from the official wrapper (intentional)
- **Explicit project form** per endpoint instead of 404-then-retry trial/error.
- **Typed structs** instead of `map[string]any` + `.get()` chains.
- **Typed `*APIError`** (status + body) surfaced, not swallowed by broad `except`.
- **One generic paginator** (`paginate`) instead of per-method token loops.
- **Split, lazy auth** — many features need no ADC.
- Rule companion `.yaml` stores a **typed deployment subset** for deterministic
  round-trips (legacy stored the raw API dict).

---

## Wave 2 — finish the `secops-wrapper` (v0.44.x) surface *(mostly landed)*

Most of this surface has **already landed as `chronicle/*.go` files** (`case` ·
`alert` · `entity` · `ingest` · `stats` · `nl_search` · `gemini` · `data_export` ·
`investigations` · `watchlist` · `retrohunt` · `rule_exclusion` · the `*_write.go`
writers · …); the remaining gap is **CLI verbs over the already-built SDK** plus the
few unbuilt files below. Per-file status is in [CATALOG.md](CATALOG.md). Read the
matching `third_party/secops-wrapper/src/secops/chronicle/*.py` when implementing.

- **Rule writes & lifecycle** (`rules.go`/`rule_exclusion.go`/`rule_retrohunt.go`):
  UpdateRule (etag), DeleteRule, enable/alerting toggles, retrohunts
  (create/get/list), rule exclusions (+ deployment, activity), list detections,
  list errors, search rule alerts.
- **Entities & IoCs** (`entity.go`): SummarizeEntity (IP/domain/hash/user),
  ListIoCs (Mandiant prioritization).
- **Cases & alerts** (`case.go`, `alert.go`): get/list/patch/merge cases, get/
  update/bulk-update alerts, bulk case ops (tag/assign/priority/stage/close/reopen).
  Note: one case has two ids — a UUID on the Chronicle API and an integer on the
  SOAR AppKey API (same record, not two cases); the reliable path is SOAR (Wave 3).
- **Investigations** (`investigations.go`).
- **Reference-list / data-table / feed / parser / dashboard WRITES**: create/
  update/delete + replace-rows, parser run/copy/activate, parser extensions,
  dashboard create/import/add-chart/execute-query. Each extends its Wave-1 file.
- **Ingestion** (`ingest.go`): IngestLog, IngestUDM, ImportEntities.
- **Forwarders** (`forwarders.go`), **log-processing pipelines** (`log_pipeline.go`).
- **Data export** (`data_export.go`): create/get/list/cancel, available log types.
- **Watchlists** (`watchlists.go`).
- **Analytics & AI**: `stats.go` (get_stats), `nl_search.go` (NL→UDM + search),
  `gemini.go` (query_gemini, opt-in), `log_types.go` (list/classify/describe).

Cross-cutting to add in Wave 2: per-resource etag round-trip on updates, view
enums (rule/reference-list/dashboard), and a streaming/`--as-list` pagination
helper for very large lists.

---

## Wave 3 — features the wrapper does NOT cover

Kept generic and tenant-neutral. **Full design:
[`docs/SOAR-DESIGN.md`](SOAR-DESIGN.md)** — read it before implementing.

- **SOAR (`soar/`)** — one host, one AppKey, **no ADC**. The **AppKey legacy external
  API (`/api/external/v1`) is the reliable, most-complete surface** and backs the
  reconcile engine (14 surfaces) + the `soar case` verbs; the **modern v1alpha
  methods are new, pull/patch-only, and 500 intermittently**. The tiers:
  - **Legacy AppKey** `soar/legacy/` — the durable, broad SDK the engine runs on
    (reliable). *Not* a quarantine; see [SOAR-DESIGN.md](SOAR-DESIGN.md).
  - **Bridge** `legacyPlaybooks:legacy*` — v1alpha host, legacy op names; the one
    genuinely delete-when-native-ships piece. Gotchas: UUID rotates on save
    (re-resolve by name), int→str coercion, name charset, whole-body replace.
  - **Modern** v1alpha native — integrations · connectors · jobs · grouping ·
    cases; pull + patch only today (flaky), not the primary build path.
  - `soar (modern) → soar/internal/transport ← soar/legacy` (modern never imports legacy).
- **Chronicle `legacy:` verbs** `chronicle/legacy.go` (ADC) — `legacyFindRawLogs`,
  `legacyBatchGetCases` (SOAR integer-id ⇄ SIEM uuid map). Modern v1alpha endpoints
  that carry a `legacy` path segment — New-generation, NOT the Siemplify external API.
- **Connectors & cron jobs**: connector/job instance configs pulled/patched via the
  v1alpha SOAR surface; scheduled runners (case hygiene) — generic scaffolding here,
  kept tenant-neutral.
- **Config secret-at-rest (planned)** — `secopsctl config` writes
  `~/.secopsctl/instance.yaml` (`0600`, git-ignored). v1 stores the SOAR AppKey in
  **plaintext**. v2: encrypt the AppKey at rest bound to the current OS user —
  Windows DPAPI, macOS Keychain, Linux libsecret/Secret Service — decrypted
  in-process at run time. Needs per-OS implementation **and cross-platform tests
  (Linux, Windows, macOS)** before it ships; until then plaintext + `0600` is the
  documented behavior. The mintable OAuth token stays out of the file entirely.

### Wave 3 build-out — SOAR external API (Siemplify `/api/external/v1`) full surface

**Why the external API (not keyless-over-ADC):** the modern v1alpha SOAR methods on
`*-chronicle.googleapis.com` (`generateSoarAuthJwt`, `soarDomains.list`,
`integrations`) require the caller to be a **workforce-identity-federated SOAR
user**; a plain ADC/OAuth token (even with `roles/chronicle.soarAdmin`) is rejected
by the SOAR backend (404/500). The official Python `secops` SDK hits the same wall.
So the **AppKey-authenticated Siemplify external API is the path that works** for
real tenants — and it is by far the most complete surface.

**Reference spec:** `third_party/siemplify-swagger.json` (fetched from
`app.siemplify-soar.com/swagger/v1/swagger.json`) — *Chronicle SOAR API*,
OpenAPI 3.0.1, **448 paths / 484 operations / 27 tags**, global security
`AppKey` (header), base `/api/external/v1`. This is the authoritative map for what
to implement. Goal: **support as many users/operations as feasible**, built on the
existing `soar/legacy` tier + `soar/internal/transport` (External, AppKey).

> **Swagger caveat — trust the shape, not the nullability.** The spec is an
> accurate map of paths and field *names*, but its `nullable: true` annotations on
> collection fields are unreliable: e.g. `CreateManualCase` marks
> `entities`/`playbooks`/`tags` nullable, yet the server NPEs (500) if they arrive
> as `null` — they must be sent as `[]`. When a write 500s with the generic
> `errorCode 2000`, diff your request against the **real UI request** (browser
> dev-tools) before assuming the endpoint is broken; the difference is often an
> omitted-vs-empty collection or a value the swagger claims is optional.

Priority order (config + automation that fits pull → diff → push; skip UI/runtime
noise like Homepage, CommandCenter, Agents, Reports, Dashboards):
1. **Connectors** (9) — CRUD, cards, templates, fetch-sample-data, statistics.
2. **Jobs** (10) — installed/templates, instances CRUD, run.
3. **Integrations** (9) — installed integrations, instance config + settings.
4. **Playbooks** (45) — CRUD, export/import, enable/disable, categories.
5. **Ontology** (18) — entity mappings/relations (config-as-code).
6. **Case Management** (135) — automation subset: close, comment, tag, assign, queue.
7. **Settings** (89) — config subset: environments, networks, blacklists.

**Discipline (Wave-3 testing):** smoke-test live with the AppKey; **read endpoints
broadly** (safe), **write endpoints minimally** with create → verify → **delete only
what we created** (every write hits a live production instance). **Do NOT `git push`
until the user confirms.**

---

## The path to the final secopsctl — forward waves (4+)

**Definition of done.** An operator runs *all* of Google SecOps as code and triages
live data from the terminal: full config-as-code (every reconcilable surface, both
products) with safe pull → diff → push + prune + drift-detection; full operational
triage (query → guarded act) for cases/alerts over the **reliable** path; reliable
against Google's flaky new APIs; secure (secret-at-rest, leak guard, no token on
disk); and operable (CI, releases, completions, docs). Each wave ends **validated**
(read round-trips clean; a gated write-smoke ran on an inert throwaway) with its
CATALOG rows moved forward and design docs updated in the same change.

**Waves are done strictly in order** — one wave fully built **and validated** (its
CATALOG rows moved forward, its design docs updated) before the next begins. The
number *is* the sequence. Per-wave: **Goal · Scope · Exit · Docs.** Live status per
surface lives in [CATALOG.md](CATALOG.md).

### Wave 4 — Case + alert triage (SOAR AppKey — the reliable lane)  *(done — reads live-validated; all 9 case verbs live-validated end-to-end)*
- **Goal.** Finish the daily triage workflow on the path that is reliable. Small,
  unblocked, high-value — the SDK already exists, this is CLI plumbing + validation.
  (Same case, two APIs: this wave uses the reliable SOAR AppKey lane; the Chronicle
  UUID API reaches the same case but is flaky.)
- **Scope.** Wire the missing **reads** on the AppKey lane: `soar case list`
  (`ListCaseCards`; `--status`/`--limit`/`--json`) and `soar case get <id>`
  (`GetCaseFullDetails` → the case **and its alerts**). Complete the query → review →
  act loop over the verbs already built (`soar case` mutates + `soar push bulk-close`).
- **Exit.** Live read-validated; act verbs dry-run-validated (live mutate on a
  throwaway-safe case only).
- **Docs.** SOAR-DESIGN, SIEM-DESIGN (the cases-are-one-case bridge), CATALOG.

### Wave 5 — SIEM config plane onto the engine  *(done — `data_tables`/`parsers`/`feeds`/`dashboards`/`rule_exclusions` on the engine + `curated` toggles + SIEM write-smoke harness; read **and write** live-validated)*
- **Goal.** Turn SIEM config-as-code from one surface into the whole plane.
- **Scope.** Wire `data_tables` → `feeds` → `parsers` → `dashboards` → `curated`
  (read + enable/disable) onto the shared reconcile engine; add a SIEM write-smoke
  harness (`SECOPS_SIEM_SMOKE` / `_WRITE`). `data_tables` first — its
  `ReplaceDataTableRows` is a wholesale destroy-and-replace, exactly what the
  dry-run guard is for. Resolve the `feeds` `assetNamespace`(read)/`namespace`(write)
  mismatch with a live smoke before wiring it; `parsers` are immutable (Create+Delete).
- **Exit.** Each surface pulls clean + a gated write-smoke passes; CATALOG → ✅.
- **Docs.** SIEM-DESIGN (plan → built), CATALOG, ARCHITECTURE §3.

### Wave 6 — Rules as code (finish the one bespoke surface)  *(done — `rules-update`/`rules-deploy` + `rules` detections/errors/alerts/retrohunt; lifecycle write-smoke passed)*
- **Goal.** Full rule lifecycle as code (rules stay bespoke: YARA-L source + a
  deployment state machine, not one canonical body).
- **Scope.** Over the existing `rules_write`/`rule_exclusion`/`retrohunt`/`rule_results`
  SDK: update (etag), enable/alerting toggles, retrohunts, exclusions, list
  detections/errors, search rule alerts. New `push rules-update` etc.
- **Exit.** Live read-validated + a gated write-smoke on a throwaway rule.
- **Docs.** SIEM-DESIGN, CATALOG.

### Finishing the "done" waves — write-validation gaps

A few write paths in shipped waves were built + read-validated but not yet
live-write-validated. Status after closing them:

- **Wave 5 — `parsers`**: now ✅ — gated write smoke `TestLiveReconcileParserWriteSmoke`
  runs `RunParser` (pure inert logic validation — no server state) then creates a new
  **INACTIVE** version from a real active parser's source, asserts it never goes ACTIVE
  (so live ingestion is untouched) and that the borrowed log type's active parser is
  unchanged, then deletes the throwaway. Deliberately skips `activate` — the only
  ingestion-affecting call. The `RunParser` response decodes `parsedEvents` as an
  object `{events:[{event:…}]}`, not a bare array.
- **Wave 5 — `reference_lists`**: now ✅ — write live-validated. There is no delete (and
  no archive) API, so the smoke can't be a throwaway-and-delete; instead
  `TestLiveReconcileReferenceListWriteSmoke` reuses one fixed, clearly-labeled inert
  list and drives a create-or-reuse + one update each run (fresh description + entries
  → always-present update, rerunnable, no accumulation). Reconcile identity is kept
  stable: create echoes the project NUMBER in the returned resource name while list
  echoes the project ID, so the SDK normalizes both to the id form (the engine keys
  identity on the name).
- **Wave 5 — `feeds`**: ✅ — write live-validated (incl. GCS V2 / Storage Transfer
  Service); short-`logType` expansion fixed; `FetchFeedServiceAccount` added.
- **Wave 4 — `soar case` verbs + `bulk-close`**: now ✅ — live-validated end-to-end by
  `TestLiveSOARCaseVerbsWriteSmoke` (create two throwaway cases → rename/describe/
  importance/priority/tag/untag/stage → merge → close). The legacy server doesn't
  null-guard `entities`/`playbooks`/`tags`, so omitting them returns a 500 *after*
  creating the case (the UI sends those as `[]`). `CreateManualCase` is typed
  (`ManualCaseRequest`, returns the new case id) and always sends those collections as
  `[]`; the transport does not retry non-idempotent POSTs on 5xx (a retry would
  duplicate the half-created case). `merge` needs the target id inside `casesIds` (the
  CLI adds it); hard delete (`RetentionDeleteCases`) is 403 for the AppKey role, so the
  smoke cleans up by closing (re-run-tolerant; a retention grant would make it
  zero-residue).

### Wave 7 — SOAR completion  *(done — reads + connectors/jobs writes live-validated; form-dynamic-parameters deferred)*
- **Goal.** Close SOAR to full config-as-code.
- **Scope.** Finish the remaining write-smokes + enable `--prune` where a clean
  delete-by-id exists; **ontology** raw lane (entity mappings/relations, export/import
  bundles); `connectors`/`jobs`/`integrations` full reconcile (beyond pull+patch);
  dynamic-case config; remaining settings surfaces.
- **Live-test focus.** `connectors`/`jobs` — validate the write path live, safely:
  an **idempotent same-value PATCH** (re-send current config, no change) proves the
  patch round-trip without disturbing a live integration; secrets read back masked
  must pass through unchanged.
- **Exit.** SOAR CATALOG rows ✅ or documented read-only-by-choice; ontology covered;
  `connectors`/`jobs` write live-validated.
- **Docs.** SOAR-DESIGN, CATALOG.
- **Status (done — live-validated).** `connectors` and `jobs` moved off the flaky
  modern v1alpha pull+patch onto the reliable legacy AppKey **reconcile engine**
  (`soar pull/push connectors|jobs`) — reconcile lane 14 → 16. All 16 SOAR reconcile
  surfaces **read** round-trip clean live (`TestLiveReconcileReadAllSOAR`); `jobs`
  **write** is live-validated (`TestLiveReconcileJobWriteSmoke`: throwaway disabled
  job → engine update → delete); `connectors` **write** is live-validated full CUD
  (`TestLiveReconcileConnectorWriteSmoke`: throwaway DISABLED connector from a
  template → engine create → update → delete). Connector **create** works by OMITTING
  the `identifier` (the server assigns one; a client-assigned id routes to the update
  path and 404s) + supplying the mandatory params. **Prune sweep**: `PruneEligible` on
  `connectors`, `visual-families`, `networks` (joining `webhooks`). **Integration instances** (no
  update endpoint) → imperative `soar integration create`/`delete`; **singleton
  case-routing policies** → imperative `soar settings case-assignment`/`move-case-policy`
  (`get`/`set`). **Ontology** mapping rules (selector read + batch upsert + body
  delete) + export/import bundles stay the raw lane — covered, not a reconcile fit;
  remaining settings (RBAC/SSO, config-items, system singletons) are
  read-only-by-choice/raw. **`form-dynamic-parameters` investigated but deferred** —
  its strict PUT update silently resets a parameter's `formType` to Invalid (dropping
  it out of its form) even with the UI's integer-enum body, so reconcile update is
  unsafe; reachable read-only via the raw lane. **Integration management** (modern
  v1alpha): `soar integration list`/`uninstall` (custom packs) and
  `soar integration connector list`/`delete` (custom connector **definitions** —
  e.g. a duplicated "Copy of …" template) round out the surface; whole-integration
  delete is custom-only, and the installed-vs-catalog model is documented in
  [SURFACES.md](SURFACES.md).

---

The forward waves below are **derived from a full audit** of the legacy swagger and
the Chronicle v1alpha docs against the SDK (see [SURFACES.md](SURFACES.md) for the
per-family gap map). The legacy SOAR external API is ~99.8% wrapped; the remaining
work is almost entirely **modern v1alpha SIEM** surfaces.

**Ordered by risk × value so we build strictly in sequence** (the number *is* the
order — no skipping): the stable-plane SIEM surfaces first, lowest-risk first —
read-only Threat Intel to prove the new-surface pattern, then the config-as-code
completions (curated rules → RBAC → Content Hub → ingestion). The **flaky-backend**
surfaces come after, behind clean-error-on-500 guards (Chronicle UUID operational,
then SOAR v1alpha lifecycle), with the reliable lanes staying the default. Cross-cutting
hardening, distribution, and automation close out.

### Live v1alpha surface status *(`soar/version_probe_live_test.go`, `chronicle/version_probe_live_test.go`)*

What each modern v1alpha surface returns **right now**, by host. This drives the
Wave 13 upgrade. "Works" = a live read returned 2xx (not yet a reliability
guarantee — v1alpha can 500 intermittently, so legacy stays the fallback).

| Surface | SIEM host (chronicle, ADC) | SOAR host (siemplify, AppKey) | Note |
|---|---|---|---|
| threatCollections · curatedRules · iocs:find · riskConfig · dataAccessLabels · dataAccessScopes · forwarders | ✅ works | (n/a) | SIEM-plane; iocs:find needs the `fieldAndValue` body, riskConfig is `{instance}/riskConfig` |
| marketplaceIntegrations · contentHub/contentPacks | ⛔ **500** | ✅ works (405 / 59) | Content Hub is **SOAR-host**, not chronicle |
| cases | (UUID API flaky) | ✅ works (69 KB) | upgrade candidate |
| alertGroupingRules | (n/a) | ✅ works | already modern |
| environments · socRoles · customLists · caseStage/Close/Tag/QueueDefinitions | (n/a) | ✅ works | on legacy reconcile lane today |
| dataAccessLabels · dataAccessScopes | ✅ works | ⛔ 404 | SIEM-plane only |
| playbooks · workflows | (n/a) | ⛔ 404 | legacy-only, no v1alpha |

### Wave 8 — Threat Intelligence (SIEM read)  *(done — `threatCollections` list/get (`ti`) + the `iocs find`/`get` CLI, read-validated live (`chronicle/ti.go`, pinned v1). The enrichment RPCs `:fetchRelated`/`:fetchEntityMetadata`/`:fetchIocMatchMetadata` are deferred as optional gaps — see SURFACES)*
- **Goal.** Mandiant / Applied Threat Intelligence as code — read the campaigns,
  reports, actors, malware and IoCs the tenant is matched against. Read-only (TI is
  Google/Mandiant-sourced — there is no write path), so low-risk and high-signal —
  the first wave, and the one that proves the new-surface pattern end to end.
- **Scope.** `threatCollections` list/get (+ `:fetchRelated`/`:fetchEntityMetadata`/
  `:fetchIocMatchMetadata`); modern `iocs:find`/get/batchGet. SIEM plane,
  `chronicle/ti.go`. Operational lane (query → review), `--limit`-capped.
- **Exit.** Live read round-trip on `threatCollections` + `iocs:find`; CATALOG ✅.
- **Docs.** SIEM-DESIGN, SURFACES, CATALOG.

### Wave 9 — Curated-rules-as-code completion  *(done — `ListCuratedRules`/`GetCuratedRule` (187 rules) read-validated; `BatchUpdateCuratedRuleSetDeployments` live-validated by a self-restoring toggle write-smoke (enable→verify→restore, alerting off))*
- **Goal.** Make curated (Google-managed) detections fully diff-and-push-able.
- **Scope.** `curatedRuleSetDeployments:batchUpdate` (the atomic write primitive for
  a desired-state curated-deployment file); `listCuratedRules`/`getCuratedRule`;
  `legacyRunTestRule` (dry-run a rule against historical data); add `archived` to the
  custom-rule deployment update. SIEM plane, `chronicle/curated.go` + `rules_write.go`.
- **Exit.** A curated-deployment file reconciles via one batch call; `RunTestRule`
  read-validated; CATALOG ✅.
- **Docs.** SIEM-DESIGN, SURFACES, CATALOG.

### Wave 10 — SIEM RBAC & data governance  *(done — `dataAccessLabels` + `dataAccessScopes` CRUD and `riskConfig` get + idempotent update all write-validated by self-cleaning smokes; operated **imperatively, not reconcile** (create→list lag + create-despite-error break diffing). No CLI yet — SDK only)*
- **Goal.** Manage access control as code — the highest-value SIEM config still
  missing.
- **Scope.** `dataAccessLabels` + `dataAccessScopes` full CRUD operated
  **imperatively** (not the reconcile engine — the surface has create→list lag and
  create-despite-error, which break desired-state diffing); `riskConfig`
  get/update (imperative singleton); BigQuery-export config. SIEM plane,
  `chronicle/rbac.go`.
- **Exit.** Labels/scopes pull clean + a gated reconcile write-smoke on a throwaway;
  risk-config read/write validated.
- **Docs.** SIEM-DESIGN, SURFACES, CATALOG.

### Wave 11 — Content Hub (modern, **SOAR host**)  *(done — Content Hub served on `*.siemplify-soar.com` (AppKey, v1alpha), NOT chronicle.googleapis.com; `marketplaceIntegrations` (405) + `contentHub/contentPacks` (59) read-validated; **install/uninstall live-validated** via a self-cleaning install→uninstall round-trip on an inert pack — the modern `:uninstall` makes it cleanly reversible. SDK only — the mutations are deliberate ops, no CLI)*
- **Goal.** Manage installable content on the durable SOAR-host API — the twin of
  the legacy `/store` install path, and the only place an **uninstall** exists.
- **Scope.** `marketplaceIntegrations` list/get/install/uninstall; `contentHub.
  contentPacks` list/get/add/delete. SOAR plane, `soar/marketplace.go`
  (`*.siemplify-soar.com`, AppKey, v1alpha — the chronicle host 500s). Imperative
  (install/uninstall) + raw where bundled. The separate chronicle-side *featured
  content* surface stays distinct.
- **Exit.** Read-validated; a gated install→uninstall smoke on a throwaway pack.
- **Docs.** SIEM-DESIGN, SURFACES, CATALOG.

### Wave 12 — SIEM ingestion completion  *(done — forwarders wired as a reconcile surface + engine write-smoke (create→update→delete) live-validated; collectors read; schema discovery (`feedSourceTypeSchemas`/`logTypeSchemas`) read-validated (`chronicle/schemas.go`) as the basis for feed-YAML validation; `GetLogType`/`GetLogTypeSetting` built)*
- **Goal.** Ingestion config-as-code beyond feeds/parsers.
- **Scope.** `forwarders` + `forwarders.collectors` full CRUD (reconcile);
  `feedSourceTypeSchemas`/`logTypeSchemas` discovery (validate feed YAML before
  deploy); `logTypes` get/update setting. SIEM plane.
- **Exit.** Forwarders/collectors pull clean + gated write-smoke; schema discovery
  drives feed validation.
- **Docs.** SIEM-DESIGN, SURFACES, CATALOG.

### Wave 13 — Make modern the default in the CLI; `--legacy` to force legacy *(in progress)*
- **Goal.** `secopsctl` uses the **modern v1alpha API by DEFAULT** for each surface
  that has been validated, auto-falling back to the reliable legacy AppKey path on
  error; a **`--legacy` flag forces the legacy path only** (skip modern). **Keep both
  SDK tiers** — legacy stays importable and is both the auto-fallback and the
  `--legacy` target. Per the project policy in CLAUDE.md.
- **Three phases, applied per surface (don't flip a surface's default until it
  passes phase 1):**
  1. **Validate** — a live modern read, and for writes a gated write-smoke on the
     modern path. A single 200 is a *candidate*; a passing smoke *promotes* it.
  2. **Flip default to modern** — the command calls modern first and **auto-falls
     back to legacy** on transport error / 5xx (and 404 host-mismatch). Remove the
     interim opt-in `--modern` flag (e.g. on `soar case list`) once modern is the
     default — it becomes the standard behavior.
  3. **`--legacy` escape hatch** — a **global persistent flag** (alongside `--json`)
     that forces the legacy AppKey path only, skipping modern — for when v1alpha is
     flaky, for parity checks, or to pin a known-good path. A shared
     `preferModern(modernFn, legacyFn)` helper centralizes the try-modern /
     fallback / `--legacy`-short-circuit logic so every surface behaves identically.
- **Surfaces + state:**
  - **cases** — modern read validated (interim `soar case list --modern`); flip to
    default + honor `--legacy`. Case *verbs/writes* stay legacy until the modern
    verbs pass a write-smoke (modern case verbs are the historically flaky ones).
  - **Content Hub / alertGroupingRules / integrations / connectors / jobs** — no
    legacy equivalent (or modern is already the path); default is modern already.
  - **environments · socRoles · customLists · caseStage/Close/Tag/QueueDefinitions** —
    modern reads validated; the reconcile lane still runs on legacy → flip to modern
    with legacy fallback (optional / lower priority; legacy engine already works).
- **Done so far:** the global **`--legacy`** flag + shared **`preferModern`** helper;
  **`soar case list` flipped to modern-by-default** with legacy auto-fallback
  (`--modern` opt-in removed; `--status` filtered client-side; live-validated incl.
  `--legacy`); Content Hub CLI (`soar marketplace`); modern read coverage for the
  config surfaces. **Remaining:** flip the config-surface reconcile lane (optional);
  promote case verbs once their modern write-smoke passes.
- **Exit.** Validated surfaces are modern-by-default with working legacy auto-fallback;
  `--legacy` forces legacy everywhere; SURFACES tracks the per-surface default;
  legacy SDK retained.
- **Docs.** SURFACES (per-surface host/version/default), CATALOG, ARCHITECTURE §6/§7.

### Wave 14 — Chronicle (UUID) operational API + remaining SIEM operational  *(modern cases reachable on the SOAR host (`soar case list`, v1alpha); the chronicle-host UUID cases collection 500s on all of v1/v1beta/v1alpha (confirmed) — flaky alternate, not needed. `watchlists list/get` CLI wired (v1, read-validated); `alerts` read remaining (needs a snapshot query))*
- **Goal.** Reach cases/alerts/events through Chronicle's newer **UUID API** — the
  **same** cases the SOAR AppKey lane already operates, not a separate system —
  lighting it up if/when those endpoints stabilize, behind a clean-error-on-500
  guard. Reliability-gated; the reliable case path stays the SOAR AppKey lane. Also
  wire the remaining SIEM operational surfaces.
- **Scope.** `cases` act/bulk (v1beta, the UUID API onto the same case),
  `alerts list/get/update/bulk`, `stats`, `search nl`, `entity summarize`,
  `iocs list`; wire `watchlists`/`forwarders`/`log_pipelines` (SDK present);
  reviewed-`--ids` + `--filter` dry-run-first + `--limit` caps. Per-endpoint
  version pinned + tracked in ARCHITECTURE §6.
- **Live-test focus.** `alerts` (read + gated update/bulk) and
  `watchlists`/`forwarders`/`log_pipelines` — live read round-trip, plus a gated
  write-smoke on a self-cleaning throwaway for each (where a delete/teardown
  exists). Where the Chronicle `cases` UUID API answers, validate reads; mutations
  stay gated and 500s fail clean.
- **Exit.** Reads validated where the API answers; the listed surfaces' writes
  validated or documented why not; mutations gated; 500s fail clean.
- **Docs.** SIEM-DESIGN, ARCHITECTURE §6, CATALOG.

### Wave 15 — SOAR v1alpha lifecycle *(reliability-gated)*  *(SDK built — alertGroupingRules create/delete + connector/job instance runOnDemand; live-write pending (v1alpha flaky), legacy lane stays default)*
- **Goal.** Close the modern-SOAR lifecycle gaps the legacy lane can't cover — only
  where the legacy API has no equivalent, and only once the v1alpha endpoints stop
  500ing.
- **Scope.** `connectorInstances`/`jobInstances` create/delete + `:runOnDemand`;
  `alertGroupingRules` create/delete (completing list/get/patch). SOAR-modern plane.
  Each method clean-error-on-500; the reliable legacy lane stays the default.
- **Exit.** Where the endpoint answers, read + gated write validated; 500s fail clean
  and the legacy path remains the documented default.
- **Docs.** SOAR-DESIGN, SURFACES, ARCHITECTURE §6, CATALOG.

### Wave 16 — Reliability & safety hardening
- **Goal.** Production-grade trust.
- **Scope.** Per-endpoint version-pinning audit (the §6 map kept current) — the pins
  now live in one place (`chronicle/versions.go`, the `APIVersions` map) behind the
  surface-family registry (`internal/mirror/surface_families.go`) and a drift-guard
  test that fails if code, §6, and the registry disagree; the audit re-probes
  **every** surface across v1 > v1beta > v1alpha (`chronicle/version_probe_live_test.go`)
  and re-pin each to the newest that works, **re-validating writes** on the new
  version before flipping (the major existing surfaces — rules / reference_lists /
  data_tables / feeds — already read OK on v1, but their write-smokes ran on v1alpha,
  so re-validate then re-pin to v1); **drift-detection mode** (`pull` + diff + report, no push — a CI gate); etag/conflict
  everywhere; request-id surfaced on every error; pagination/`--as-list`; **config
  secret-at-rest** (Windows DPAPI / macOS Keychain / Linux libsecret) decrypted
  in-process, **with cross-OS tests**.
- **Exit.** Secret-at-rest shipped + tested on 3 OSes; drift mode runnable in CI.
- **Docs.** ARCHITECTURE, ROADMAP.

### Wave 17 — Distribution & operability
- **Goal.** Easy to install and run anywhere.
- **Scope.** CI (build/test/lint/`govulncheck`/`semgrep`); release binaries
  (goreleaser); `secopsctl version`; shell completions; man pages; `doctor`
  enhancements; packaging (brew/scoop).
- **Exit.** A tagged release with signed binaries; CI green on PRs.
- **Docs.** README, ROADMAP.

### Wave 18 — Automation & scheduling *(stretch)*
- **Goal.** Tenant-neutral scheduled automation scaffolding.
- **Scope.** Generic scheduled runners (case-hygiene jobs); the LLM-driven automation
  notes in `tips/`; scheduled drift reports.
- **Docs.** ROADMAP, `tips/`.

---

## Non-goals
- No bundled tenant identifiers, rule names, or secrets — ever (tenant-neutral).
  A pre-commit leak guard (`.githooks/pre-commit`) enforces this; when porting
  logic from a private source, bring over only generic, sanitized code.
- No third-party EDR (e.g. SentinelOne) or chat/notification (e.g. Teams)
  integrations — out of this repo's scope.
- No silent overwrite of concurrent edits — honor etag, surface conflicts.
- `push` is never non-interactive-by-default — dry-run first, explicit `--yes`.
