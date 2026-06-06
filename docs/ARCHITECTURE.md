# Architecture

How `secopsctl` works, independent of any one surface. Product specifics are in
[SOAR-DESIGN.md](SOAR-DESIGN.md) / [SIEM-DESIGN.md](SIEM-DESIGN.md); what exists
and its status is in [CATALOG.md](CATALOG.md).

## 1. Two planes

| | Control plane | Operational plane |
|---|---|---|
| **Subject** | desired-state *config* | live *data* |
| **Examples** | rules, lists, tables, feeds, parsers, dashboards, SOAR automation | events, alerts, cases |
| **Loop** | `pull` → review in `git diff` → `push` (reconcile) | `query` → review → `act` |
| **Source of truth** | the git repo | the live instance |
| **Files?** | yes — one per object, diffable | no — live data isn't snapshotted as desired state |
| **A mutation is…** | a production *deploy* | a production *action* (triage) |

They never mix. You don't reconcile a case from a file; you don't triage a
detection rule. One CLI, two models.

## 2. The reconcile engine (control plane)

A single product-neutral engine (`internal/mirror/reconcile`, imports no SDK)
drives every config surface. A surface is a declarative descriptor; the engine
does the orchestration.

```
Surface{ Name, Dir, Caps,
         List(ctx)→[]Object,            // live state
         LoadDir(dir)→[]Object,         // local files
         Write(dir,Object),             // pull writer
         Create/Update/Delete(...) }    // the CUD ops
```

- **Object** = `{ Slug, ServerID, Etag, Canonical, Raw }`. `Canonical` is the
  diff basis: redacted + volatile-stripped + deterministically serialized, so a
  `git diff` (and the push plan) shows only real config changes.
- **Identity = `ServerID`.** Matching local↔live is by server id, never by slug —
  so non-unique display names, rotating UUIDs (playbooks key on *name*), etc. are
  handled. `Pull` disambiguates colliding filenames with the id.
- **Plan** = classify each object: `Create` (local-only), `Update` (id matches,
  canonical differs), `Delete` (live-only — a *prune candidate*), `Unchanged`.
- **Push** = additive by default (Create+Update). `--prune` is required to Delete,
  gated on a `PruneEligible` surface **and** a complete pull; otherwise server-only
  objects are warned, skipped, and **reprinted in a final summary** (long logs hide
  mid-stream warnings). Dry-run is the default; a real apply needs `--yes` under a
  `LIVE DEPLOY` banner.
- **Redaction round-trip.** Secrets are redacted on pull (`***REDACTED***`). A push
  never sends the marker back: **Update overlays edits onto the live body and drops
  masked fields** (keeping the real secret); **Create refuses** a body that still
  carries a marker.
- **Capabilities** adapt behavior without the engine inspecting payloads:
  `WholeBodyWrite`, `NoDelete`, `NoEtag`, `PruneEligible`.
- The **`jsonSurface` adapter** turns any RawJSON per-object endpoint into a
  Surface from a few JSON-path + method params — this is what makes adding a
  surface a one-struct change. Typed surfaces (reference lists, playbooks) get a
  bespoke Surface but reuse the same engine.

## 3. The lane model

Every surface is **exactly one lane**. Classification is empirical (verified
against the API's *response* schema + a live read), not guessed from a method
name — that distinction is what kept the engine honest.

| Lane | Fits | Mechanism |
|---|---|---|
| **reconcile** | clean per-object CUD: stable id, read-shape ≈ write-shape, delete-by-id | the engine + a Surface |
| **raw** | batch upserts, export/import bundles, selector-only reads, read≠write | `soar legacy call` (pull JSON → edit → guarded post) |
| **imperative** | per-entity verbs, no desired-state file | a command tree (`soar case`, `curated`) |
| **operational** | live data: query a subset, act on it | query + act commands (§4) |
| **skip** | runtime/UI/telemetry, singletons, auth topology | not modeled |

The engine **enforces** the boundary: a batch/bundle/selector endpoint cannot
register as a reconcile surface. When the swagger response is grouped/nested or
the write body is an array, it's `raw`, not `reconcile`.

## 4. The operational model (query → review → act)

Live data (events/alerts/cases). The SDK is largely built; the design is the
operator model and its **safety**.

- **Query.** Every `list`/`search` shares a **filter**, a **time window**, a
  **`--limit`** (with a default, so a query never pulls the whole tenant), and an
  output: a compact **table** for humans, **`--json`** as the contract that pipes
  into an act command.
- **Act — per item.** Unambiguous, low blast radius: `<domain> <verb> <id> …`.
- **Act — subset (the dangerous one), two paths, safest first:**
  1. **Reviewed-ids** (preferred): `list --json | … > ids` → `bulk <verb> --ids @ids`
     — you act on exactly what you reviewed.
  2. **Filter-in-one-shot** (gated harder): `--filter` is **always dry-run-first**
     (prints match count + a sample, refuses to mutate) and **`--limit`-capped**.
- **One guard rule:** an operational mutation is a production deploy — `LIVE`
  banner, dry-run default, `--yes`. Events are immutable telemetry: **read-only,
  never mutate**. A case is **one record with two ids** (Chronicle UUID, SOAR
  integer) — operate it on the reliable SOAR AppKey lane, not two case systems.

## 5. Cross-cutting

- **Auth is split and lazy** (`auth/`). SIEM = ADC/OAuth (token, minted in-process,
  never on disk). SOAR = AppKey (in the `0600` config or `$SECOPS_SOAR_APP_KEY`,
  no ADC). Credentials resolve on the first request — `--help`/offline never touch
  ADC. The SIEM token honors `gcloud` ADC or `SECOPS_ACCESS_TOKEN`.
- **etag / optimistic concurrency.** Mutating paths round-trip the stored etag;
  on mismatch, surface a clean conflict — never silently overwrite a concurrent
  edit (a teammate's UI change, a parallel push).
- **Reliability.** The official **new** APIs (Chronicle v1alpha/v1beta REST,
  modern SOAR v1alpha) **500 intermittently** — Google is still building SecOps.
  Validate new surfaces against the **reliable** paths (SOAR AppKey, stable SIEM
  reads) + the **swagger**, not the flaky live API. On a 500: fail cleanly with
  the request id; retry idempotent **reads**, never a **mutation** (double-apply
  risk).
- **Build discipline (how a surface earns "validated").** Swagger-spec the shape →
  **verify SDK signatures by hand** (the spec agents are imprecise) → wire the
  Surface/command → **live read-validate** (pull round-trips clean) → **gated write
  smoke** on a uniquely-labeled, inert, self-deleting throwaway. No `--yes` path is
  trusted until that smoke passes. Status lands in [CATALOG.md](CATALOG.md).

## 6. API versions — per endpoint, tested not guessed

SecOps uses **different API versions for different endpoints**, and Google moves
them (an endpoint that answered `v1beta` yesterday may need `v1` today, or 500 at
all of them). So the version is **not** a global constant and **not** a user flag:
each endpoint family pins **its own** version in the SDK (a `const`, e.g.
`caseAPIVersion`), set to **the version that works**. When an API moves or stops
responding, change the const to the version that works and update this table.
**This table is the record — keep it current.**

| Endpoint family | Version | Status | Notes |
|---|---|---|---|
| SIEM config + reads — rules · reference_lists · data_tables · feeds · parsers · dashboards · search · entity | `v1alpha` | ✅ | `DefaultAPIVersion`; doctor + pulls confirm |
| Chronicle **cases** (UUID) API — get/list/patch/merge/bulk | `v1beta` | ⛔ | the **same case** as the SOAR AppKey row below, via Chronicle's newer API; v1 / v1alpha / v1beta all 500 or hang intermittently (server-side) — **not** the working case path. Operational case work uses the reliable SOAR AppKey row below; one case, two APIs |
| SIEM legacy case reads — `legacy:legacyListCases` · `legacyBatchGetCases` | `v1alpha` | ⛔ | `legacyListCases` 404; `legacyBatchGetCases` is the SOAR-int ⇄ SIEM-uuid bridge |
| SOAR legacy — `/api/external/v1/…` (**cases** · connectors · jobs · settings · playbooks bridge) | external `v1` · AppKey | ✅ | the reliable path — **incl. the working operational case lane** (`GetCaseCardsByRequest`, `GetCaseFullDetails` → alerts, `ExecuteBulkCloseCase`, `ChangeCasePriority`) |
| SOAR modern — integrations · connectors · jobs · grouping | `v1alpha` | 🔨 | pull + patch |

Principle: **test → hard-code the working version per family → record it here.** No
per-user version flag; the SDK ships the version that works, and this table tracks
which is which (and what's currently down).
