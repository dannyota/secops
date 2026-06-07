# SOAR design — config-as-code lanes over the reliable AppKey API

Design for the Google SecOps **SOAR** surface of `secopsctl`, ported from real
operational SOAR usage. Two facts drive it:

> **1 · The AppKey legacy API is the backbone, not a quarantine.** The *modern*
> v1alpha SOAR methods are new, 500 intermittently, and cover little; the AppKey
> `/api/external/v1` surface is reliable and by far the most complete, so it is what
> the operator-facing **reconcile engine** runs on (`soar/legacy/` — ~90 files, a
> durable SDK). Only the genuinely transitional `legacyPlaybooks:legacy*` *bridge*
> is "delete when the native API ships."
>
> **2 · Every surface is exactly one lane** — reconcile (per-object CUD), imperative
> (per-entity verbs, no file), or raw (batch/bundle/selector passthrough). The
> engine + lane model is product-neutral and lives in
> [ARCHITECTURE.md](ARCHITECTURE.md); this doc is SOAR specifics. Live status per
> surface is in [CATALOG.md](CATALOG.md).

All identifiers here are placeholders (`<tenant>`, `<num>`, `<reg>`, `<id>`) — the
public repo stays tenant-neutral; real values come from config/env at runtime.

## The operator surface — three lanes

What an operator actually drives. Every surface is classified into one lane, and the
engine enforces the boundary (a batch/bundle/selector endpoint *cannot* register as
reconcile):

| Lane | Mechanism | SOAR surfaces |
|---|---|---|
| **reconcile** (per-object CUD) | engine + a `reconcile.Surface` | **16**: `webhooks` · `environments` · `networks` · `tracking-lists` · `soc-roles` · `idp` · `visual-families` · `sla-definitions` · `case-stages` · `case-tags` · `close-root-causes` · `blacklists` · `playbook-categories` · `playbooks` (bespoke, name-keyed) · `connectors` (Wave 7) · `jobs` (Wave 7). (`form-dynamic-parameters` was investigated and deferred — its PUT update is unsafe; see CATALOG) |
| **imperative** (per-entity verbs, no desired-state file) | `soar case <verb>` · `soar integration` · `soar settings` | cases: `list` (New API — siemplify v1alpha, auto-falls back to the Legacy queue) · `get <id>` (case + its alerts); 9 mutate verbs (assign · rename · stage · tag · untag · describe · importance · close · merge) + `soar push bulk-close`. integration **instances** (no update endpoint → not reconcilable): `integration create` / `delete`. integration **packs/definitions** (New API — siemplify v1alpha): `integration list` / `uninstall` (custom packs only) and `integration connector list` / `delete` (custom connector **definitions** — e.g. a "Copy of …" duplicate). singleton case-routing policies: `settings case-assignment` / `move-case-policy` (`get`/`set`) |
| **raw** (batch upserts / bundles / selector reads) | `soar legacy call <op>` | integrations (reads) · ontology-mapping (selector read + batch upsert + body delete) · environment-priorities · permissions · system/singleton settings · … |

Commands:

- `soar pull <surface>` — live → files
- `soar push <surface> [--prune]` — files → live; additive by default, dry-run unless `--yes`
- `soar case list`/`get` — read
- `soar case <verb>` — guarded mutate
- `soar legacy call <op>` — raw passthrough

The operational loop is **`soar case list` → review → `soar case get <id>` → act**
(a mutate verb or `soar push bulk-close`). Per-surface identity, capabilities (NoDelete /
WholeBodyWrite / PruneEligible) and read/write validation are in
[CATALOG.md](CATALOG.md). `PruneEligible` (a clean, low-blast delete-by-id, so
`--prune` may delete server-only objects) is set on `webhooks`, `connectors`,
`visual-families`, and `networks`; every other surface is additive/NoDelete by design — its delete takes a body selector, or is high-blast
(an environment orphans its cases) or RBAC/SSO-sensitive (`idp` has a clean by-id
delete but pruning a forgotten mapping would silently revoke a group's access).

## The transport tiers (under the hood)

The lane table above is what an operator drives; this is the transport reality the
lanes ride on. SOAR uses one host (`https://<tenant>.siemplify-soar.com`) with
**one AppKey and no ADC**, across three tiers:

| Tier | Surface | Transport | Lifecycle |
|---|---|---|---|
| **Modern** (New API) | v1alpha native on the **siemplify** domain: integrations · connectors · jobs · alertGroupingRules · moduleSettings · cases | `/v1alpha/projects/<num>/…/instances/<id>` + `?format=camel` + `x-goog-api-version` + `updateMask` | siemplify serves **v1alpha only**. Validated where it adds something Legacy lacks — **cases `list`** runs here (`soar.ListCases`, auto-falls back to the Legacy queue) and the Content Hub / integration reads. **Connectors/jobs config-as-code runs on the Legacy AppKey reconcile engine (Wave 7);** `alertGroupingRules`/`moduleSettings` stay here via `soar pull grouping` |
| **Bridge** 🟠 | `legacyPlaybooks:legacy*` (list/get/save/attach/stats) | v1alpha host, **legacy op names** | the one genuine *quarantine*: delete when native v1alpha **playbook CRUD** ships |
| **Legacy AppKey** ✅ | Siemplify external API — the broad, reliable surface the **reconcile engine** runs on | `/api/external/v1/…` (offset paging) | **durable backbone**, not slated for removal |

> **Which tier to trust:** the **Legacy AppKey** tier is reliable and complete and
> backs the engine; the **Modern (New API)** v1alpha tier on the siemplify domain is
> newer and used per-surface where it's validated and adds something Legacy lacks
> (cases `list`, Content Hub / integration reads), falling back to Legacy on error.
> Only the *Bridge* tier is genuinely delete-when-native.

Plus one **legacy SIEM** pair on the Chronicle side (ADC auth, not SOAR):
`legacy:legacyFindRawLogs` and `legacy:legacyBatchGetCases` (the SOAR-integer-id
⇄ SIEM-uuid bridge).

## Package layout

```
danny.vn/secops/
│
├── auth/                         OAuth(ADC)  +  APIKey/SOARAppKey      ← unchanged
├── config/                       + soar_url (tenant SOAR host)         ← small add
│
├── chronicle/   (SIEM · v1alpha · MODERN, ADC)
│   └── legacy.go   🗑 QUARANTINE FILE  ── legacyFindRawLogs, legacyBatchGetCases
│                   (SOAR int-id ⇄ SIEM uuid map). Delete when v1alpha equivalents land.
│
└── soar/   (host=https://<tenant>.siemplify-soar.com · AppKey, NO ADC)
    │
    │   internal/transport/        shared, durable plumbing (AppKey + host) — transport.go
    │     • Transport.V1Alpha() → /v1alpha/projects/<num>/locations/<reg>/instances/<id>/…
    │                      auto: ?format=camel · x-goog-api-version · updateMask · {items,nextPageToken}
    │     • Transport.External() → /api/external/v1/…   offset paging {requestedPage,pageSize}
    │
    ├── MODERN — v1alpha native (pull/patch only · flaky) ─────────────────────
    │     client.go        SOAR client
    │     integrations.go  integrations(list/get/delete) · connector defs(list/get/delete) · jobs   (discovery + custom cleanup)
    │     connectors.go    connectorInstances   GET · PATCH(updateMask) · :fetchLatestDefinition
    │     jobs.go          jobInstances         GET · PATCH(updateMask)
    │     grouping.go      alertGroupingRules · moduleSettings(:batchUpdate)
    │     cases.go         cases   (v1alpha listing)
    │
    └── soar/legacy/   ── DURABLE AppKey SDK (~90 files) — backs the reconcile engine ──
          ─ reliable Siemplify external API (/api/external/v1); NOT a quarantine ─
          cases · connectors · jobs · settings · ontology · webhooks ·
          environments · networks · blacklists · soc-roles · …
          ─ BRIDGE (the one delete-when-native piece): v1alpha host, legacy op names ─
          legacyPlaybooks:legacy{List,Get,GetByName,Save,Attach,Stats}
                               gotchas baked in (see below)

   dependency rule:  soar(modern)  →  soar/internal/transport  ←  soar/legacy
                     (modern never imports legacy; both share the transport)
```

## Wire shapes actually sent — modeled as types

```
legacy/cases.go     CaseQueueRequest{ SortBy, RequestedPage, PageSize, Statuses[] }   // 1=OPEN 2=CLOSED
                    BulkCloseRequest{ CasesIDs[], CloseReason, RootCause, CloseComment, DynamicParameters[] }
                      └ CloseReason enum: 0 NotMalicious · 1 Malicious · 2 Maintenance · 3 Inconclusive
connectors/jobs     Parameters map[string]string   // EVERYTHING is a string ("true","100")
                      └ secrets read back as "***…" → pass through unchanged on PATCH (never re-send a real secret)
transport (v1alpha) every request: ?format=camel  +  header x-goog-api-version: v1alpha  +  PATCH ?updateMask=a,b
bridge/playbooks    coercePlaybookTypes(): id/priority/version/*UnixTimeInMs int→str (top-level, trigger, each step)
                    validatePlaybookName(): allow [A-Za-z0-9 _-], reject . ( ) [ ] : /
                    playbook save mints a NEW UUID → never cache it; re-resolve by display name
                    save = whole-body replace (not a patch): read → modify same body → save
```

## SOAR-specific gotchas to encode

- **Playbook UUID rotation** — every save mints a new identifier; the one you sent
  goes stale. Always re-resolve by display name after a save.
- **Playbook type coercion** — GET returns ints, save requires strings for
  `id`/`priority`/`version`/`*UnixTimeInMs` (top-level, `trigger`, each `step`);
  `templateName` must be `""`, never `null`.
- **Playbook name charset** — letters/digits/space/`-`/`_` only; reject `.()[]:/`.
- **One case, two ids** — a case is a single record reachable by two APIs: the
  SOAR AppKey API (integer id, the reliable path secopsctl uses) and the Chronicle
  API (UUID, same case, currently flaky). Map between the ids via
  `legacy:legacyBatchGetCases` (`soarPlatformInfo.caseId`) only when correlating —
  not two case systems.
- **Parameters are always strings** on connectors/jobs (even bool/int); secrets
  read back masked (`***…`) and must be passed through unchanged on PATCH.
- **Installed vs catalog integrations** — each marketplace pack lists twice: the
  per-tenant **installed** copy has an `identifier` of `<base>__<uuid>` (with
  `productionIdentifier` = the base) and is what the tenant runs; the bare
  `<base>` entry is the catalog/base definition. Both report `custom:false`, so
  neither is whole-deletable (`integrations.delete` is custom-packs-only) — that
  protects the working installs. A duplicated connector **definition** inside a
  pack (`custom:true`, e.g. a "Copy of …") is removed per-definition via
  `DeleteConnectorDef`, not by deleting the pack.
- **Two paginations** — legacy is offset (`requestedPage`/`pageSize`); v1alpha is
  Google-style (`pageToken`/`nextPageToken`).

## Repo touchpoints beyond the SDK

- **config:** add `soar_url` (tenant SOAR host); reuse `project_number`/`region`/
  `customer_id` for the v1alpha path. AppKey via `auth.SOARAppKey` /
  `SECOPS_SOAR_APP_KEY` (no ADC).
- **internal/mirror + internal/cli:** `pull soar` (cases · playbooks · connectors ·
  jobs · grouping → YAML/JSON snapshots) and guarded `push` (bulk-close ·
  connector-patch · job-patch · playbook-save) under the same dry-run / LIVE-DEPLOY
  guard as rules.
- **leak guard:** SOAR snapshots carry masked secrets + integer case IDs; the
  pre-commit scanner already covers AppKey/secret patterns.

## Out of scope (per project decision)

No SentinelOne, no Teams/chat notifications — out of this repo's scope.
