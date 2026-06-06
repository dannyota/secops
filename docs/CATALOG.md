# Catalog & status

The source of truth for **what exists and how mature it is**. Every surface
carries a status; update it in the same commit that moves the surface forward.
Design in [ARCHITECTURE.md](ARCHITECTURE.md); product specifics in
[SOAR-DESIGN.md](SOAR-DESIGN.md) / [SIEM-DESIGN.md](SIEM-DESIGN.md).

**Where the code is:** surfaces register in `internal/mirror/registry_{soar,siem}.go`
(playbooks: `soar_playbooks.go`); the product-neutral engine is
`internal/mirror/reconcile`; live write-smokes are gated by `SECOPS_SOAR_SMOKE`
(read) / `SECOPS_SOAR_SMOKE_WRITE` (write) in `reconcile_smoke_test.go`.

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
| `rules` | bespoke | 🔨 | 🔨 | YARA-L source + deployment state machine (two resources) — `push rules-create/disable`; not a single canonical body |
| `reference_lists` | reconcile | ✅ | 🔨 | typed, `.txt`+`.yaml`; NoDelete; engine = product-neutral |
| `data_tables` | reconcile | ✅(pull) | 📐 | `.csv`+`.yaml`; rows via separate `ReplaceDataTableRows` |
| `feeds` | reconcile | 🔨(pull) | 📐 | secrets in `settings`; **resolve `assetNamespace`(read) vs `namespace`(write) with a live smoke first** |
| `parsers` | reconcile | 🔨(pull) | 📐 | versioned/immutable → Create + Delete only |
| `dashboards` | reconcile | 🔨(pull) | 📐 | native dashboards, charts as JSON; full CUD |
| `curated` / `curated_rules` | read+toggle | 🔨(pull) | 📐 | Google-managed → read + enable/disable/alerting + exclusions, not CUD |
| `watchlists`, `rule_exclusions`, `forwarders`, `log_pipelines` | reconcile | — | 📐 | SDK present; wire where per-object CUD fits |

## SIEM · operational plane (query → act)

| Domain | Query | Act | Status | Notes |
|---|---|---|---|---|
| **events (UDM)** | `query udm` · `search nl` · `stats` | — | 🔨 query udm · 📐 rest | immutable telemetry — **read-only** |
| **alerts** | `alerts list/get` | `alerts update/bulk` (verdict/priority/status/comment) | 📐 | SIEM-native SDK built (`GetAlerts`/`UpdateAlert`/`BulkUpdateAlerts`). In practice operators read alerts as a **field of the SOAR case** (`GetCaseFullDetails.alerts`) — the reliable view |
| **cases** (SIEM-native, UUID) | `cases list/search/get` | (planned) | 🔨 read · ⛔ API | the SIEM-*native* v1beta/v1alpha collection returns intermittent 5xx/404 — a **secondary** unified view, **not** the working path. **Operational case work runs on the reliable SOAR AppKey lane** (next row) — same case, bridged by `soarPlatformInfo.caseId`. See SOAR-DESIGN |
| **entities / IoCs** | `entity summarize` · `iocs list` | — | 📐 | enrichment — read-only |

## How to keep this current

When a surface advances: edit its row here **and** the relevant design doc in the
**same commit**. A surface reaches `✅` only after a live read round-trips clean and
(for writes) a gated smoke passed on an inert throwaway — see the build discipline
in [ARCHITECTURE.md](ARCHITECTURE.md) §5. A `⛔` row records *what* is blocked and
*why* (which API, which error) so it can be retried, not silently forgotten.
