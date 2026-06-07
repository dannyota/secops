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
| `rules` (YARA-L) | source `.yaral` + deployment state machine | **done** (bespoke two-resource, not a single canonical body): `push rules-create` · `rules-update` (etag-guarded YARA-L text update of changed `.yaral`, validated first) · `rules-deploy` (reconcile each companion's deployment — enabled/alerting/runFrequency — the deployment state machine as code) · `rules-disable`. Plus operational `rules detections/errors/alerts <id>` and `rules retrohunt list/get/create`. Read live-validated; lifecycle write smoke (create→update→deploy→delete, self-cleaning) |
| `reference_lists` | typed, `.txt`+`.yaml` | **done + live-validated** (engine, NoDelete): resource-name **normalization** in the SDK — create echoes the project NUMBER in the returned name while list echoes the project ID, so both are rewritten to the id form (otherwise the engine, which keys identity on the name, sees a freshly-created list and the same listed list as two objects). Gated write smoke `TestLiveReconcileReferenceListWriteSmoke`: since there is no delete API it can't be a throwaway-and-delete, so it reuses one fixed, clearly-labeled inert list and drives a create-or-reuse + one update each run (fresh description + entries → always-present update, rerunnable, no accumulation; the list is left in place by design) |
| `data_tables` | `.csv`+`.yaml`, rows via a separate API | **done + live-validated** (engine, `push data_tables`): columns immutable after create (update rejects a column change); rows = wholesale `ReplaceDataTableRows`; not prune-eligible (whole-table delete is high-blast). Gated write smoke `TestLiveReconcileDataTableWriteSmoke` (create→update→replace rows→delete) |
| `feeds` | typed, secrets in `settings` | **done** (engine, `push feeds`): redact on pull + DeepMerge-overlay on update (real secret preserved, create refuses a masked body); `details` replaced wholesale on PATCH (WholeBodyWrite). The `assetNamespace`(read) vs `namespace`(write) mismatch is **resolved** — a live read confirmed the API uses `assetNamespace`; the write side now emits the same key. Server/migration keys stripped from canonical; feed state left as a runtime `:enable`/`:disable` toggle (out of desired state). Short `logType` is expanded to the full resource name on write (the API rejects a bare id). Read + write live-validated, incl. GCS **V2** feeds (`GOOGLE_CLOUD_STORAGE_V2` / `gcsV2Settings`, Storage-Transfer-Service-backed — grant the SA from `FetchFeedServiceAccount` read access to the bucket first) |
| `parsers` | versioned/immutable (create new, no update) | **done** (engine, `push parsers`): immutable → the Update closure is create-new-version + **activate** (the load-bearing step), old version left inactive for rollback; canonical = `{log_type, cbn}` (volatile parser id excluded, carried as ServerID + written back on refresh); live set derived from feeds; not prune-eligible. Read + write live-validated: gated write smoke `TestLiveReconcileParserWriteSmoke` runs `RunParser` (pure inert validation — no server state created or activated) then creates a new **INACTIVE** version from a real active parser's source (a unique trailing comment makes it a distinct version), asserts it never becomes ACTIVE (so live ingestion is untouched) and the borrowed log type's active parser is unchanged, then deletes the throwaway. The `RunParser` response struct was corrected — `parsedEvents` is an object `{events:[{event:…}]}`, not a bare array |
| `dashboards` (native) | typed, charts as JSON | **done** (engine, `pull`/`push dashboards`): CUSTOM only (CURATED excluded as read-only); one `<slug>.json` (config + `_server`), charts inline under `definition.charts` replaced wholesale on update; `access` immutable after create. Canonical strips `createUserId`/`updateUserId`/`dashboardUserData` + the root resource `name` (identity in ServerID); nested chart/filter ids are stable across reads so they need no stripping. `pull` re-pointed from export-envelope → config. Read live-validated; gated write smoke. NB: the full-view List can rate-limit (429) on instances with many dashboards — the write smoke drives the surface closures directly rather than repeatedly listing |
| `curated` / `curated_rules` | Google-managed (read-mostly) | **done** (imperative `curated list`/`set`, NOT reconcile): the catalog is fixed (no create/delete) and the batch write body is an array, so it's the imperative lane, not the engine. `set` toggles `enabled`/`alerting` per (category, rule set, precision precise\|broad) via a guarded single PATCH (new `chronicle/curated_write.go`, updateMask-driven). Live-validated via a guarded enable→disable toggle that restores prior state. Rule **exclusions** are their own `rule_exclusions` reconcile surface (findingsRefinements, full CUD) — separate from curated |
| `rule_exclusions` (findings refinements) | typed: display_name + type + UDM query | **done** (engine, `pull`/`push rule_exclusions`): Create + Update (PATCH/updateMask); NoDelete (no delete API), NoEtag; deployment toggle out of the diff basis. Read + write live-validated (create→update→archive); no hard delete exists — **archive** (deployment `archived=true`) is the documented teardown. This is the "exclusions" the curated row points to |
| `watchlists`, `forwarders`, `log_pipelines` | typed | engine surfaces where per-object CUD fits |

**Discipline (same as SOAR, proven):** workflow-spec the shape → verify SDK
signatures by hand → wire as a `reconcile.Surface` → **live read-validate** →
**gated write-smoke** on an inert throwaway. The SIEM write-smoke harness lives in
`internal/mirror/reconcile_smoke_siem_test.go`, gated by `SECOPS_SIEM_SMOKE` (read
round-trip of every SIEM surface) and `SECOPS_SIEM_SMOKE_WRITE` (the
create/update/delete cycle). No surface is trusted for `--yes` until its write loop
is live-validated.

---

## Plane 2 — Operational query/act (the SOC workflow)

This is the part that needs the new design. Events/alerts/cases are **live
security data**, not desired state. The loop is **query → review → act on each or
a subset** — exactly how an analyst triages. The SDK is largely already built;
this plane is about the *operator model* and *safety*, not new API code.

### The three act surfaces (and one read-only)

| Surface | Query (read) | Act (mutate) | Mutability |
|---|---|---|---|
| **events (UDM)** | `SearchUDM` / `NLSearch` / `GetStats` / `FindUDMFieldValues` | — | immutable telemetry — **read-only, never mutate** |
| **alerts** | `GetAlerts` (list) · `GetAlert` · `ListDetections` · `SearchRuleAlerts` | `UpdateAlert` · `BulkUpdateAlerts` (status / verdict / priority / reason / comment) | per-item + subset |
| **cases** (one case; operated via `soar case`, see below) | `ListCases` / `SearchCases` · `GetCase` | `PatchCase` · `MergeCases` · `BulkClose/Assign/AddTag/ChangePriority/ChangeStage/Reopen` | per-item + subset |
| **entities / IoCs** | `SummarizeEntity` · `ListIoCs` · `FetchAssociatedInvestigations` | — | enrichment — read-only |
| **threat intel** | `ListThreatCollections` · `GetThreatCollection` (`ti collections`/`collection`) | — | Mandiant-sourced — **read-only** (no write path) |

> **Threat Intelligence** (`threatCollections`) is a read-only operational surface:
> the Google/Mandiant campaigns, reports, actors, malware and vulnerabilities the
> tenant is matched against. The list takes a `collection_type:` filter
> (campaign/report/…), an `orderBy`, and is `--limit`-capped; get is by short id
> (e.g. `report--26-10031441`). It uses the regional host and the project **number**
> in the resource name. There is no TI write path — custom intel is ingested as
> normal logs + reference lists, and Applied-TI detections ship as curated rule sets.

> **There is ONE case, reached on multiple paths — not multiple case systems.**
> Google SecOps = Chronicle (SIEM) + Siemplify (SOAR) merged, so a case is a
> **single record** reachable several ways. It carries two ids and
> `legacyBatchGetCases` returns `soarPlatformInfo.caseId` linking them. The cases
> function is **live-validated**: `soar case list` defaults to the modern **New API
> on the siemplify domain (v1alpha)** and auto-falls back to the broad, reliable
> **Legacy** external API (`/api/external/v1`, AppKey) on error; `soar case <verb>`
> and `get` run on Legacy. The **only** dead path is the chronicle-host UUID cases
> collection (`chronicle.googleapis.com`, ADC), which 500s at every version — an
> alternate, unused route to the same case, not the function's status.
>
> | | New API · siemplify *(default for `list`)* | Legacy external · siemplify *(reliable; verbs + fallback)* | chronicle-host UUID *(alternate, unused)* |
> |---|---|---|---|
> | id | integer (e.g. `234`) | integer (e.g. `234`) | UUID (resource name) |
> | api · auth · version | modern `cases` · AppKey · **v1alpha** | `/api/external/v1/cases` · AppKey | v1/v1beta/v1alpha `cases` · ADC |
> | today | **live-validated** | mature, **reliable**, complete | 500s at every version |
> | CLI | `soar case list` (default) | `soar case list`/`get` · `soar case <verb>` | — |
>
> Same case, two ids — `soarPlatformInfo.caseId` bridges them when you need to
> correlate across paths. The chronicle-host UUID route is **not** a separate or
> preferred surface; it is the same case via a path that does not answer, recorded
> here as a note.

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

> **One case, reached on multiple paths.** Case operations run on the siemplify
> domain: `soar case list` defaults to the modern **New API (v1alpha, live-validated)**
> with auto-fallback to the broad, reliable **Legacy** external API; `soar case <verb>`
> plus the `soar case get` read (`GetCaseFullDetails`, which also returns the case's
> **alerts**) run on Legacy. That loop is wired and is what to use. The chronicle-host
> (UUID) `cases` collection below reaches the **same case** through `chronicle.googleapis.com`
> (ADC) but 500s at every version — it is not a separate or preferred surface, just an
> alternate route, recorded as a note.

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
  query/act (live).
- No `--yes`-by-default anywhere; `--filter` bulk is always dry-run-first.
