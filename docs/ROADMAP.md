# secopsctl / Go SDK — Roadmap

`secopsctl` is both a CLI and an importable, unofficial Go SDK for Google SecOps
(`danny.vn/secops/chronicle`). This roadmap tracks what is built and what is
planned. The guiding rule: **design cleanly, port the parity slice first, then
finish the surface** — improving on the official Python wrapper where it is weak
(see the `// DEVIATION:` markers in code).

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

Future SecOps products are **sibling packages** so `chronicle` stays focused:
`danny.vn/secops/soar`, `/sentinelone`, `/notify`.

---

## Wave 1 — parity with the legacy Python tool ✅ (current)

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

## Wave 2 — finish the `secops-wrapper` (v0.44.x) surface

Each item becomes a `chronicle/*.go` file (names reserved in `doc.go`) plus, where
it makes sense, a CLI verb. Read the matching `third_party/secops-wrapper/src/secops/chronicle/*.py`
when implementing.

- **Rule writes & lifecycle** (`rules.go`/`rule_exclusion.go`/`rule_retrohunt.go`):
  UpdateRule (etag), DeleteRule, enable/alerting toggles, retrohunts
  (create/get/list), rule exclusions (+ deployment, activity), list detections,
  list errors, search rule alerts.
- **Entities & IoCs** (`entity.go`): SummarizeEntity (IP/domain/hash/user),
  ListIoCs (Mandiant prioritization).
- **Cases & alerts** (`case.go`, `alert.go`): get/list/patch/merge cases, get/
  update/bulk-update alerts, bulk case ops (tag/assign/priority/stage/close/reopen).
  Note: SIEM case ID is a UUID; SOAR uses a separate integer ID (Wave 3).
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

- **SOAR (`soar/`)** — one host, one AppKey, **no ADC**, split into three tiers so
  legacy is a clean delete when modern APIs land (the governing principle —
  *quarantine legacy, never mix it with the durable modern code*):
  - ✅ **Modern** v1alpha native (the keeper): integrations · connectors · jobs ·
    alertGroupingRules · moduleSettings · cases.
  - 🟠 **Bridge** `soar/legacy/playbooks.go` — `legacyPlaybooks:legacy*`; remove
    when native v1alpha playbook CRUD ships. Gotchas: UUID rotates on save (re-resolve
    by name), int→str coercion, name charset, whole-body replace.
  - 🗑 **Legacy** `soar/legacy/` — Siemplify external API (`/api/external/v1`):
    cases-queue bulk-close, comment/tag/priority, playbook export/import; remove
    when v1alpha bulk-case + playbook endpoints ship.
  - `modern → soar/internal/transport ← legacy` (modern never imports legacy).
- **Legacy SIEM** `chronicle/legacy.go` (ADC) — `legacyFindRawLogs`,
  `legacyBatchGetCases` (SOAR integer-id ⇄ SIEM uuid map); quarantined file.
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
what we created** (the live tenant is production). **Do NOT `git push` until the
user confirms.**

---

## Non-goals
- No bundled tenant identifiers, rule names, or secrets — ever (tenant-neutral).
  A pre-commit leak guard (`.githooks/pre-commit`) enforces this; when porting
  logic from a private source, bring over only generic, sanitized code.
- No third-party EDR (e.g. SentinelOne) or chat/notification (e.g. Teams)
  integrations — out of this repo's scope.
- No silent overwrite of concurrent edits — honor etag, surface conflicts.
- `push` is never non-interactive-by-default — dry-run first, explicit `--yes`.
