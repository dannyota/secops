# Glossary

Plain-language definitions of the terms used across these docs and the CLI. You
do **not** need any of this to use `secopsctl` for everyday work — start at the
[project README](../README.md). This page is here for when a doc or a `--help`
string uses a word you don't recognize.

## The core idea

| Term | What it means for you |
|---|---|
| **as code** | You keep your SIEM/SOAR configuration in plain files under git, the way Terraform keeps cloud infra. You edit files, review the `git diff`, and deploy. |
| **the core loop** | `pull` (download live config to files) → review the `git diff` → `push` (deploy your edits back). This is how every config surface works. |
| **pull** | Read-only. Snapshots live config into local files. Never changes anything on the instance. |
| **push** | A live production deploy. Defaults to a preview (`--dry-run`); nothing changes until you add `--yes`. |
| **dry run** | A preview of what a `push`/act would do, without doing it. The default for every mutating command. |
| **`LIVE DEPLOY` banner** | The warning printed before a real (`--yes`) mutation, so you always know you're about to change a production instance. |
| **reconcile** | What `push` does on a config surface: compare your local files to live, then create/update (and, only with `--prune`, delete) so live matches your files. |
| **`--prune`** | Opt-in flag that lets `push` **delete** live objects that have no local file. Off by default — without it, `push` only creates and updates. |
| **drift** | When live config has changed out from under your files (e.g. someone edited it in the UI). `pull` then `git diff` shows it. |

## The two products and how you reach them

| Term | What it means for you |
|---|---|
| **SIEM (Chronicle)** | The detection side: rules, reference lists, data tables, feeds, parsers, dashboards, UDM event search. Authenticated with **ADC** (Google login). |
| **SOAR (Siemplify)** | The case/automation side: cases, playbooks, connectors, jobs, webhooks, environments. Authenticated with an **AppKey**. Reached with `secopsctl soar …`. |
| **two hosts, two credentials** | SIEM and SOAR are separate services on separate hosts with separate auth — that's why you set up an ADC login *and* an AppKey. `secopsctl` picks the right one per command automatically. |
| **ADC** | *Application Default Credentials* — your Google login, set up with `gcloud auth application-default login`. Used for all SIEM commands. The token is minted in memory; nothing is written to disk. |
| **AppKey** | A long-lived SOAR API key you generate once in the SOAR UI (Settings → Advanced → API Keys). Used for all `soar` commands. Stored `0600` in your config or passed via `$SECOPS_SOAR_APP_KEY`. |

## Words you'll see in the design docs

| Term | What it means for you |
|---|---|
| **control vs operational** | Two kinds of work. **Control** = config you keep as files (`pull`/`push`). **Operational** = live data you triage but don't keep as files — events, alerts, cases (`query`/`act`). |
| **plane** | Used two ways in these docs: (1) control vs operational (the config-vs-data split above); (2) in SURFACES.md, a *host + auth* combo — SIEM, SOAR-legacy, SOAR-modern. Same word, two axes. |
| **lane** | How a given surface is *operated*: **reconcile** (file-based create/update/delete), **imperative** (per-item verbs like `soar case close`), **raw** (advanced passthrough), **operational** (query then act), or **skip** (not modeled). You rarely need this unless you're reading the design rationale. |
| **surface** | One manageable family — e.g. `rules`, `feeds`, `webhooks`, `connectors`. The [CATALOG](CATALOG.md) lists every surface and its status. |
| **surface-family registry** | The machine-readable spine behind the CATALOG: one declarative entry per family (host, auth, API generation, version, lane, status) in `internal/mirror/surface_families.go`. A drift-guard test keeps it, the version pins, and the docs in agreement, so the status tables can't quietly fall out of date. You only meet this if you're reading the design docs. |
| **the reconcile engine** | The shared internal machinery that powers `push` for every config surface, so they all behave identically (same diff, same `--prune` guard, same secret handling). |
| **canonical** | The cleaned-up form of an object used for diffing — secrets redacted, volatile server fields stripped — so `git diff` shows only real config changes, not noise. |
| **etag** | A version stamp on an object. `secopsctl` sends it back on edits so it can refuse to overwrite a change someone else made in parallel (you get a clean conflict error instead of a silent clobber). |
| **Capabilities** (`PruneEligible`, `NoDelete`, `WholeBodyWrite`, `NoEtag`) | Per-surface flags that tune behavior. The one that affects you: **`PruneEligible`** means `--prune` is allowed to delete server-only objects of that surface; **`NoDelete`** surfaces never delete (drift is reported, never pruned). |
| **API generation (New vs Legacy)** | The two API generations a SOAR function can be served by — tracked as two columns in the [CATALOG](CATALOG.md). **Legacy** is the older Siemplify external API (`/api/external/v1`, AppKey); it's broad and dependable and is the permanent path the reconcile engine and case verbs run on. **New** is Google's modern REST (v1alpha on the SOAR host), still maturing and prone to intermittent server errors. `secopsctl` prefers New only where it's validated, and auto-falls back to Legacy on error; the global `--legacy` flag forces Legacy. (The same New-vs-Legacy axis is called `Generation` in the surface-family registry.) |
| **preferModern** | The internal helper that implements that dispatch for a SOAR function served by both generations: try New first, fall back to Legacy on error, or short-circuit straight to Legacy when `--legacy` is set. `soar case list` is currently the one function that's modern-by-default. |
| **AppKey path / "the reliable path"** | The Legacy SOAR API. It's the most complete and dependable surface, so case and most SOAR work runs on it. |
| **v1 / v1beta / v1alpha** | Google API versions. Newer surfaces are often only on `v1alpha`. "Prefer v1 > v1beta > v1alpha" just means "use the newest version that actually works for that endpoint." You never set this — `secopsctl` pins each Chronicle-host surface to its working version in one place (`chronicle/versions.go`). (The SOAR host serves v1alpha only, so the ladder doesn't apply there.) |

## Status markers (in CATALOG.md, SURFACES.md, ROADMAP.md)

| Marker | Meaning |
|:-:|---|
| 📐 | **designed** — spec'd, code not landed |
| 🔨 | **built** — code exists, not yet fully validated against a live instance |
| ✅ | **live-validated** — reads round-trip clean and (for writes) a safe write test passed |
| 🔒 | **read-only by choice** — the write path exists but is deliberately not exposed (too high-blast / sensitive) |
| ⛔ | **blocked** — a *specific* API path (one column + domain + version) is down server-side. It applies to that one path, never to a whole function: if any other path serves the function, the function's status stays green and the dead path is just a note. |
| ⬜ / — | planned gap / not applicable |

## Other terms

| Term | What it means for you |
|---|---|
| **Wave N** | A planned phase of work in [ROADMAP.md](ROADMAP.md). Just sequencing context — "Wave 7" tells you when a feature landed, nothing you need to act on. |
| **slug** | The filename for an object, derived from its display name. The real server id lives in the companion `.yaml`/`.json`, so renaming a slug file casually can mean delete-and-recreate on `push` — rename with care. |
| **companion YAML/JSON** | The metadata file beside a pulled object (e.g. `<rule>.yaral` + `<rule>.yaml`) holding its server id, etag, and deployment state. |
| **UDM** | *Unified Data Model* — Chronicle's normalized event schema. `secopsctl query udm '<filter>'` searches events with it. |
| **case (one case, two ids)** | A case is a single record, not two separate cases. It works on **both** the Legacy SOAR AppKey API (integer id — the broad, reliable path, `soar case`) and the New API on the **siemplify** domain (v1alpha, live-validated — `soar case list` defaults to it). There is also an alternate Chronicle-host path that addresses the same case by UUID (ADC), but that one path 500s at every version, so it isn't used. |
