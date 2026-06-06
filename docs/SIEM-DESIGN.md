# SIEM design — two planes: config-as-code + operational query/act

Design for the Google SecOps **SIEM** surface of `secopsctl`. SIEM splits into two
planes that need fundamentally different models. All identifiers here are
placeholders; the public repo stays tenant-neutral.

> **The split.** **Config** is *desired state* — rules, lists, tables, parsers,
> feeds, dashboards. It's detection-as-code: **pull → review in `git diff` →
> push**, reconciled. **Operational** is *live data* — events, alerts, cases.
> You don't reconcile a case from a file; you **query a subset and act on it**,
> the way a SOC analyst triages. Two planes, two models, one CLI.

```
secopsctl
├── CONFIG plane  (desired state, files, git)        pull → diff → push  (reconcile)
│     rules · reference_lists · data_tables · feeds · parsers · dashboards · curated …
│
└── OPERATIONAL plane  (live data, no files)          query → review → act
      events (read-only) · alerts (triage) · cases (triage) · entities/iocs (enrich)
```

Auth note: the SIEM API needs ADC / `SECOPS_ACCESS_TOKEN` (the SOAR AppKey does
NOT work here). The official v1alpha REST surface **HTTP-500s intermittently**
(Google is still building SecOps); surface a clean error on 500, never retry
forever (see `CLAUDE.md`).

---

## Plane 1 — Config as code (reconcile)

The user's framing: *"rules, curated rules, dashboards, etc. — similar things."*
They are. This plane **reuses the proven product-neutral reconcile engine**
(`internal/mirror/reconcile`, already shipped for SOAR + SIEM `reference_lists`):
identity + canonical diff + redaction + additive/`--prune` guard.

| Surface | Shape | Plan |
|---|---|---|
| `rules` (YARA-L) | source `.yaral` + deployment state machine | **bespoke** (two-resource; keep `push rules-create/disable`, extend with update/enable/retrohunt) — not a single canonical body |
| `reference_lists` | typed, `.txt`+`.yaml` | **done** (engine, NoDelete) |
| `data_tables` | `.csv`+`.yaml`, rows via a separate API | engine surface; rows = wholesale `ReplaceDataTableRows` |
| `feeds` | typed, secrets in `settings` | engine surface; redact on pull, overlay on update; **resolve the `assetNamespace`(read) vs `namespace`(write) mismatch with a live smoke first** |
| `parsers` | versioned/immutable (create new, no update) | engine surface, `Create`+`Delete` only (no update) |
| `dashboards` (native) | typed, charts as JSON | engine surface, full CUD |
| `curated` / `curated_rules` | Google-managed (read-mostly) | read + **enable/disable/alerting toggle + exclusions** only — not full CUD |
| `watchlists`, `rule_exclusions`, `forwarders`, `log_pipelines` | typed | engine surfaces where per-object CUD fits |

**Discipline (same as SOAR, proven):** workflow-spec the shape → verify SDK
signatures by hand → wire as a `reconcile.Surface` → **live read-validate** →
**gated write-smoke** on an inert throwaway (read+write, but blocked here until a
token + the v1alpha 500s settle). No surface is trusted for `--yes` until its
write loop is live-validated.

---

## Plane 2 — Operational query/act (the SOC workflow)

This is the part that needs the new design. Events/alerts/cases are **live
security data**, not desired state. The loop is **query → review → act on each or
a subset** — exactly how an analyst triages. The SDK is largely already built;
this plane is about the *operator model* and *safety*, not new API code.

### The three act surfaces (and one read-only)

| Domain | Query (read) | Act (mutate) | Mutability |
|---|---|---|---|
| **events (UDM)** | `SearchUDM` / `NLSearch` / `GetStats` / `FindUDMFieldValues` | — | immutable telemetry — **read-only, never mutate** |
| **alerts** | `GetAlerts` (list) · `GetAlert` · `ListDetections` · `SearchRuleAlerts` | `UpdateAlert` · `BulkUpdateAlerts` (status / verdict / priority / reason / comment) | per-item + subset |
| **cases (SIEM, UUID)** | `ListCases` / `SearchCases` (filter) · `GetCase` · `GetCases` | `PatchCase` (+etag/updateMask) · `MergeCases` · `BulkClose/Assign/AddTag/ChangePriority/ChangeStage/Reopen` | per-item + subset |
| **entities / IoCs** | `SummarizeEntity` · `ListIoCs` · `FetchAssociatedInvestigations` | — | enrichment — read-only |

> **SIEM cases and SOAR cases are TWO VIEWS OF THE SAME CASE — not two systems.**
> Google SecOps = Chronicle (SIEM) + Siemplify (SOAR) merged; cases originate in
> the SOAR case-management engine and are surfaced natively in the SIEM. The proof
> is in the wire: a case carries **both** ids and `legacyBatchGetCases` returns
> `soarPlatformInfo.caseId` — the bridge between them.
>
> | | SOAR case (Siemplify) | SIEM case (Chronicle/SecOps) |
> |---|---|---|
> | id | integer (e.g. `234`) | UUID (resource name) |
> | api · auth | `/api/external/v1/cases` · AppKey | v1beta `cases` + v1alpha `legacy:legacyListCases` · ADC |
> | role | the engine — alerts→case, playbooks, tasks, queue, stages, close | the first-class SecOps cases surface (get/list/patch/merge/bulk) — the convergence layer |
> | CLI | `soar case list`/`get` (read) · `soar case <verb>` (act) | `cases …` |
> | today | mature, **reliable** | newer, **flaky** (v1beta 500 / v1alpha 404 observed) |
>
> They stay **separate command trees** (different id, api, reliability), bridged by
> `soarPlatformInfo.caseId` only when correlating. The SIEM-UUID api is the
> forward-looking unified one; the SOAR-integer api is what works today.

### The query model

Every list/search command shares: a **filter**, a **time window**, a **limit**,
**pagination**, and an **output format**.

```
secopsctl query udm '<udm filter>' [--hours N | --from TS --to TS] [--limit N] [--json]   # events (exists)
secopsctl search nl  '<question>'   [--hours N] [--limit N] [--json]                       # NL → UDM → search
secopsctl stats      '<query>'      [--hours N]                                            # aggregations
secopsctl alerts list   [--filter EXPR] [--hours N] [--state OPEN|CLOSED] [--limit N] [--json]
secopsctl cases  list   [--filter EXPR] [--status …] [--priority …] [--limit N] [--json]
secopsctl entity summarize <ip|domain|hash|user> <value> [--hours N]
secopsctl iocs   list   [--prioritized] [--hours N] [--limit N] [--json]
```

- **Default output is a compact table** (id, key fields, status/time) for humans;
  `--json` emits the raw objects for scripting and **piping into an act command**.
- **`--limit` is mandatory-with-a-default** (e.g. 100) so a query never pulls the
  whole tenant by accident; large pulls require an explicit large `--limit`.

### The act model — single + subset, **safe by construction**

Two ways to act, mirroring how SOC consoles work (open one, or select rows →
bulk action). Both are **guarded exactly like `push`**: LIVE banner, **dry-run by
default**, real apply needs `--yes`.

**1. Per-item** — unambiguous, low blast radius:
```
secopsctl alerts update <id> --verdict FALSE_POSITIVE --priority LOW [--comment "…"]
secopsctl cases  comment <id> "triaged: benign"
secopsctl cases  assign  <id> --user <analyst>
secopsctl cases  close   <id> --reason NOT_MALICIOUS --root-cause "…"
```

**2. Subset (bulk)** — the dangerous one; two selection paths, **safest first**:

- **Reviewed-ids (preferred).** Query → eyeball → act on the *explicit* set:
  ```
  secopsctl alerts list --filter '…' --json | jq -r '.[].id' > ids.txt   # review the set
  secopsctl alerts bulk close --ids @ids.txt --reason FALSE_POSITIVE --yes
  ```
  The operator reviewed exactly what they're acting on. `--ids` accepts
  `1,2,3` or `@file`.

- **Filter-in-one-shot (convenient, gated harder).** `--filter` on a bulk verb is
  **dry-run-first, always**: it prints the **match count + a sample** and refuses
  to mutate until re-run with `--yes`, and a **`--limit` caps the blast radius**
  (refuse if the match set exceeds it unless `--limit` is raised explicitly):
  ```
  secopsctl cases bulk close --filter 'rule="<noisy>" AND priority=LOW' --reason FALSE_POSITIVE --dry-run
    → "MATCHES 412 cases (cap 100). Sample: …. Re-run with --yes --limit 500 to apply."
  ```

Guard summary (one rule): **no operational mutation runs without an explicit
`--yes`; any `--filter`-driven bulk shows the count + sample first and is
`--limit`-capped.** A live-data mutation is treated as a production deploy, same
as a config `push`.

### Command tree

*Designed shape. **Built today:** `query udm`, `cases list/get/search`. Everything
else here (incl. `alerts …`, `cases <verb>`, `cases bulk`) is the planned model, not
yet wired — authoritative per-command status is in [CATALOG.md](CATALOG.md).*

```
secopsctl query udm | search nl | stats | iocs list | entity summarize     # read
secopsctl alerts  list | get | update | bulk <close|verdict|priority|comment>
secopsctl cases   list | get | search | comment | assign | tag | priority | stage | close | reopen | merge
                  + cases bulk <close|assign|tag|priority|stage|reopen>     # subset (--ids/--filter, guarded)
```

---

## Cross-cutting

- **etag / optimistic concurrency** on `PatchCase` and alert updates — round-trip
  the stored etag; on mismatch surface a clean conflict (a teammate edited it),
  never silently overwrite. Same rule as config.
- **Idempotent reads, audited writes.** Reads are free; every mutation prints
  what it touched (ids + the change) so the action is reviewable after the fact.
- **Output for pipelines.** `--json` is the contract between query and act:
  `list --json | jq | bulk --ids @-`. Tables are for humans only.
- **Reliability.** On a v1alpha 500, fail the command cleanly with the request id;
  do not retry a mutation (risk of double-apply). Reads may retry idempotently.
- **No reconcile for live data.** Events/alerts/cases are not snapshotted to files
  for `git diff`; that would imply a desired state they don't have. (A read-only
  *export* of a query result to JSON is fine — it's a report, not a mirror.)

## First implementation wave — SIEM **cases** (operational)

> **Two case paths, one case.** The *SIEM-native* cases collection below (v1beta) is
> new and returns intermittent 5xx; the **reliable, complete path for case operations
> is the SOAR AppKey API** — `soar case <verb>` plus the `soar case list`/`get` reads
> (`ListCaseCards` / `GetCaseFullDetails`, which also returns the case's **alerts**).
> The SOAR reads are wired (`soar case list`/`get`), completing the reliable
> query → review → act loop; the SIEM-native `cases`/`alerts` commands here are the
> unified view for when that collection stabilizes.

Decided: the subset-act model is **both** paths (reviewed-`--ids` preferred,
`--filter` gated dry-run-first + `--limit`-capped), and the first wave is **case
management** — the full triage lifecycle.

```
# query
secopsctl cases list   [--filter EXPR] [--status …] [--priority …] [--limit 100] [--json]   # ListCases / ListCasesOpts
secopsctl cases search '<expr>' [--hours N] [--json]                                         # SearchCases
secopsctl cases get <uuid> [--expand alerts|events]                                          # GetCase

# per-item act (guarded: dry-run default, --yes to apply)
secopsctl cases comment  <uuid> "<text>"
secopsctl cases assign   <uuid> --user <analyst>
secopsctl cases tag      <uuid> --tag <t>
secopsctl cases priority <uuid> --priority <…>
secopsctl cases stage    <uuid> --stage <…>
secopsctl cases close    <uuid> --reason <…> --root-cause "<…>"
secopsctl cases reopen   <uuid> --comment "<…>"
secopsctl cases merge    --into <uuid> --ids <a,b,c>

# subset act (guarded; --ids reviewed-set OR --filter dry-run-first + --limit cap)
secopsctl cases bulk <close|assign|tag|priority|stage|reopen> [--ids 1,2|@file | --filter EXPR] … [--yes] [--limit N]
```

Wiring: single-field edits go through `PatchCase` (etag + `updateMask` — round-trip
the etag, surface conflicts); `merge` → `MergeCases`; every `bulk` verb → the
matching `Bulk*` SDK method. Reuse the `liveBanner` + dry-run/`--yes` guard from
`push`. A `cases <verb>` shares the `casesOps` plumbing; `cases bulk` adds the
`--ids`/`--filter`+`--limit` selection on top.

**Build discipline (same as SOAR, and the gate is real here):** SIEM needs a token
(`SECOPS_ACCESS_TOKEN`; ADC is restricted) and the v1alpha surface 500s — so the
query/read layer + `--dry-run` previews are built and validated first (safe), and
**no `--yes` bulk close/assign is trusted until a live smoke** closes→reopens a
single throwaway-safe case (or runs against a non-prod instance). Until a token is
available, this wave ships read + dry-run only.

## Non-goals

- No mutation of events (immutable telemetry) and no bulk **delete** of live data.
- No mixing planes: config stays reconcile (files/git), operational stays
  query/act (live). The dual SIEM-UUID / SOAR-int case worlds stay separate.
- No `--yes`-by-default anywhere; `--filter` bulk is always dry-run-first.
