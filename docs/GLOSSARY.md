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

## What you work with: rules, alerts, cases, playbooks

Six words name the chain from *what detects* to *what responds*. They are distinct
objects — except **workflow**, which is an umbrella, not an object. Keeping them
straight heads off the most common mix-up: that a "SIEM case" and a "SOAR case"
are two different things. They are not — see *one concept, not two* below.

| Term | What it is | Plane | Command |
|---|---|---|---|
| **rule** | A detection you author in YARA-L over UDM events. A match produces a *detection*; an *alerting* rule also raises an **alert**. | SIEM | `secopsctl rules` |
| **curated rule** | A detection authored and maintained by Google (curated detections / Applied Threat Intelligence), shipped in rule *sets*. You don't write the logic — you toggle enabled/alerting at set × precision only. | SIEM | `secopsctl curated` |
| **rule exclusion** | A filter that stops matching events from ever reaching rules (custom *and* curated) — noise suppression, not a rule itself. | SIEM | `secopsctl rule_exclusions` |
| **alert** | A detection raised for triage — what an alerting rule (custom or curated) produces. Alerts flow into SOAR and group into cases; an alert is then a *member of* its case. | SIEM → SOAR | `secopsctl alerts` (triage) · `soar case alert …` (as a case member) |
| **case** | The investigation record an analyst works: grouped alerts plus entities, comments, tasks, SLA, and playbook runs. **One record** — see below. | SOAR | `secopsctl soar case` |
| **playbook** | A SOAR automation: an ordered graph of actions and flow steps run against a case or alert (enrich, notify, contain). | SOAR | `secopsctl soar playbook` |
| **workflow** | *Not a distinct object* — an umbrella for SOAR automation overall: **playbooks** plus the **jobs** and **alert-grouping** rules around them. "Build a workflow" means author a **playbook**. | SOAR | (playbook / job / grouping) |

The chain in one line: a **rule** (or **curated rule**) fires on UDM events →
raises an **alert** → alerts group into a **case** → a **playbook** automates the
response.

### One concept, not two — commands that look split but aren't

A few entities are reachable on both the SIEM (Chronicle/ADC) and SOAR
(Siemplify/AppKey) hosts. That is a *transport* split — the same record behind two
doors — not two separate objects. The goal is one command per concept, with
`secopsctl` picking the host for you.

| You might think… | Reality |
|---|---|
| "a SIEM case vs a SOAR case" | **One** case. The case verbs live under `soar case` today and are being unified under a single top-level `cases` command (ROADMAP Wave 85), auto-routed to the host that serves them. (An alternate Chronicle-host path reaches the same case by UUID but is unused — it errors at every API version.) |
| "a SIEM alert vs a SOAR alert" | **One** alert. You triage it on the SIEM side (`alerts`) and act on it as a member of its **case** on the SOAR side (`soar case alert …`). Same record, two stages of one life. |
| "`iocs` vs `ti`" | Two *different* things that cross-reference. **`iocs`** resolves an indicator value seen in your environment to its IoC record; **`ti`** browses the upstream Mandiant threat-intelligence catalog (collections / campaigns / reports). `iocs related` and `ti iocs` are the bridge between them. |
| "`rules` vs `curated` vs `rule_exclusions`" | Three roles, not three copies: **`rules`** = detections you author; **`curated`** = Google-managed detections you toggle; **`rule_exclusions`** = event filters that keep noise out of both. |
| "`pull`/`push` vs `soar pull`/`soar push`" | Same core loop, two planes: top-level for SIEM config, `soar …` for SOAR config. The verbs behave identically; only the host and auth differ. |

## Words you'll see in the design docs

| Term | What it means for you |
|---|---|
| **control vs operational** | Two kinds of work. **Control** = config you keep as files (`pull`/`push`). **Operational** = live data you triage but don't keep as files — events, alerts, cases (`query` then act). |
| **plane** | Used two ways in these docs: (1) control vs operational (the config-vs-data split above); (2) in design/surfaces.md, a *host + auth* combo — SIEM, SOAR-legacy, SOAR-modern. Same word, two axes. |
| **lane** | How a given surface is *operated*: **reconcile** (file-based create/update/delete), **imperative** (per-item verbs like `soar case close`), **raw** (advanced passthrough), **operational** (query then act), or **skip** (not modeled). You rarely need this unless you're reading the design rationale. |
| **surface** | One manageable family — e.g. `rules`, `feeds`, `webhooks`, `connectors`. The [CATALOG](design/catalog.md) lists every surface and its status. |
| **surface-family registry** | The machine-readable spine behind the CATALOG: one declarative entry per family (host, auth, API generation, version, lane, status) in `internal/mirror/surface_families.go`. A drift-guard test keeps it, the version pins, and the docs in agreement, so the status tables can't quietly fall out of date. You only meet this if you're reading the design docs. |
| **the reconcile engine** | The shared internal machinery that powers `push` for every config surface, so they all behave identically (same diff, same `--prune` guard, same secret handling). |
| **canonical** | The cleaned-up form of an object used for diffing — secrets redacted, volatile server fields stripped — so `git diff` shows only real config changes, not noise. |
| **etag** | A version stamp on an object. `secopsctl` sends it back on edits so it can refuse to overwrite a change someone else made in parallel (you get a clean conflict error instead of a silent clobber). |
| **Capabilities** (`PruneEligible`, `NoDelete`, `WholeBodyWrite`, `NoEtag`) | Per-surface flags that tune behavior. The ones that affect you: **`PruneEligible`** means `--prune` is allowed to delete server-only objects of that surface; **`NoDelete`** surfaces never delete (drift is reported, never pruned); **`NoEtag`** surfaces carry no etag, so `push` can't detect a concurrent live edit of an object's fields the way etag round-tripping does — `pull` immediately before you edit one. (Dashboards are a `NoEtag` surface.) |
| **API generation (New vs Legacy)** | The two API generations a SOAR function can be served by — tracked as two columns in the [CATALOG](design/catalog.md). **Legacy** is the older Siemplify external API (`/api/external/v1`, AppKey); it's broad and dependable and is the permanent path the reconcile engine and case verbs run on. **New** is Google's modern REST (v1alpha on the SOAR host), still maturing and prone to intermittent server errors. `secopsctl` prefers New only where it's validated, and auto-falls back to Legacy on error; the global `--legacy` flag forces Legacy. (The same New-vs-Legacy axis is called `Generation` in the surface-family registry.) |
| **preferModern** | The internal helper that implements that dispatch for a SOAR function served by both generations: try New first, fall back to Legacy on error, or short-circuit straight to Legacy when `--legacy` is set. `soar case list` is currently the one function that's modern-by-default. |
| **AppKey path / "the reliable path"** | The Legacy SOAR API. It's the most complete and dependable surface, so case and most SOAR work runs on it. |
| **v1 / v1beta / v1alpha** | Google API versions. Newer surfaces are often only on `v1alpha`. "Prefer v1 > v1beta > v1alpha" just means "use the newest version that actually works for that endpoint." You never set this — `secopsctl` pins each Chronicle-host surface to its working version in one place (`chronicle/versions.go`). (The SOAR host serves v1alpha only, so the ladder doesn't apply there.) |

## Status markers (in catalog.md, surfaces.md, ROADMAP.md)

| Marker | Meaning |
|:-:|---|
| 📐 | **designed** — spec'd, code not landed |
| 🔨 | **built** — code exists, not yet fully validated against a live instance |
| ✅ | **verified** — reads round-trip clean and (for writes) a safe write test passed |
| 🔒 | **read-only by choice** — the write path exists but is deliberately not exposed (too high-blast / sensitive) |
| ⛔ | **blocked** — a *specific* API path (one column + domain + version) is down server-side. It applies to that one path, never to a whole function: if any other path serves the function, the function's status stays green and the dead path is just a note. |
| ⬜ / — | planned gap / not applicable |

## Other terms

| Term | What it means for you |
|---|---|
| **Wave N** | A planned phase of work in [ROADMAP.md](../ROADMAP.md). Just sequencing context — "Wave 7" tells you when a feature landed, nothing you need to act on. |
| **dataRoot** (a.k.a. `<dataRoot>`) | The local directory the mirror reads and writes — the current working directory by default. `pull` writes `./rules`, `./reference_lists`, `./data_tables`, etc. under it; `push` and `drift` read from it. Override with `--out` (and `--rules-dir` for the rules folder specifically). |
| **slug** | The filename for an object, derived from its display name. The real server id lives in the companion `.yaml`/`.json`, so renaming a slug file casually can mean delete-and-recreate on `push` — rename with care. |
| **companion YAML/JSON** | The metadata file beside a pulled object (e.g. `<rule>.yaral` + `<rule>.yaml`) holding its server id, etag, and deployment state. |
| **UDM** | *Unified Data Model* — Chronicle's normalized event schema. `secopsctl query udm '<filter>'` searches events with it. |
| **investigation** | The per-alert Gemini (TIN) analysis: verdict, confidence, summary, suggested next steps. `alerts investigate <de_id>` triggers one and polls it; `--latest` reads the most recent without starting one. The agent's working notes live in a *notebook* (visible under `--json`). |
| **components palette** | Everything a playbook can be built from — actions (every integration), Flow functions/operators, trigger kinds, reusable blocks. `soar playbook components <actions\|flow\|triggers\|blocks>` lists each; the [playbooks guide](guides/playbooks.md) walks it. |
| **read-only mode** | `--read-only` or `SECOPS_READONLY=1`: every guarded mutation degrades to a dry-run preview (even with `--yes`) and verbs that would start an AI generation are refused. The hard launcher-level guard for autonomous agents. |
| **audit log** | `$SECOPSCTL_HOME/audit.jsonl` (default `~/.secopsctl/audit.jsonl`): one JSONL record per **confirmed** mutation or read-only refusal — time, action, decision. Guard decisions, not server outcomes. |
| **CBN** | *Configuration-Based Normalization* — the parser language, the format of each `<LOG_TYPE>.conf` parser file `pull` writes. See the [feeds & parsers tip](tips/08-feeds-parsers.md). |
| **case (one case, two ids)** | A case is a single record, not a SIEM case and a separate SOAR case. The same record carries an integer id on the Legacy SOAR AppKey API (the broad, reliable path) and is also served by the New API (v1alpha) on the **siemplify** host; `soar case list` defaults to the New path. The bridge between the two ids is `cases soar-id`, which resolves a SIEM case UUID (an alert's `caseName`) to its SOAR integer id. There is also an alternate Chronicle-host path that addresses the same case by UUID (ADC), but it errors at every version, so it isn't used. These verbs are being unified under a single top-level `cases` command (ROADMAP Wave 85) so one record means one command. |
