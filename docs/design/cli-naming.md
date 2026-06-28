# Command naming — Phase D (hard rename, no aliases)

Phase D is a one-shot **hard rename** of the `secopsctl` command surface to a small
set of self-describing top-level groups. Every command name now says what it
operates on, related verbs are grouped under one noun, and the host/auth plane is an
implementation detail the tool resolves — never something the operator selects by
picking a command. No capability is added or removed by the rename; the same verbs do
the same work under the new paths.

Unlike the earlier clarity pass, **Phase D keeps no aliases and no back-compat.**
The old names are gone. Scripts and muscle memory move to the canonical names in one
step. This keeps `commands --json` (the machine-readable command catalog) a single,
unambiguous list — an agent keys off the canonical `path` with no alias rows to
reconcile.

It pairs with the [glossary](../GLOSSARY.md), which defines the underlying terms
(rule, curated rule, alert, case, playbook) and the *one concept, not two* rule, and
with [architecture.md](architecture.md), which explains the two planes the groups
map onto.

## Principles

1. **Clear over terse, but group by noun.** A top-level group names the subject it
   manages (`search`, `rules`, `cases`, `ingest`, `content-hub`); verbs live under it
   (`curated set`, `search saved run`). Where a short token is the established
   product word it stays short (`ti` for threat intel/IOCs, `soar`).
2. **One concept, one command.** A single SecOps record gets a single command,
   auto-routed to whichever API host serves it. The host/auth plane is spelled in
   `--help`, never in the command name.
3. **Collections are plural; multi-word names are hyphenated.** `alerts`, `cases`,
   `entities`, `lists`; `content-hub`, `data-access`, `log-types`. Matches the cobra
   convention every flag already uses.
4. **No aliases.** Each surface has exactly one runnable name. A removed name is an
   error, not a silent redirect.

## The three locked decisions

These resolve the cross-cutting ambiguities the rename exposed. They are settled and
every other doc follows them verbatim.

1. **`pull` / `push` target args stay `snake_case`; only standalone command groups
   rename.** The targets mirror the on-disk mirror tree, and Phase D renamed the
   *command* surface, not the directory layout. So `pull rules`,
   `pull reference_lists`, `pull data_tables`, `pull feeds`, `pull parsers`,
   `pull curated`, `pull rule_exclusions`, and every `push <target>`
   (`rules-create` / `rules-update` / `rules-deploy` / `rules-disable`) **keep their
   snake_case spelling** so they keep matching the directory names
   (`./reference_lists/`, `./rule_exclusions/`, …). Only the imperative/read command
   groups moved (`feeds schemas` → `ingest feeds schemas`, and so on).

2. **`cases` is the canonical top-level case command; there is no `soar case`.** Case
   triage is genuinely cross-plane (the modern v1alpha path and the reliable legacy
   AppKey path back the same record), so the triage verbs are top-level `cases …`
   (`list` / `get` / `assign` / `tag` / `close` / `comment` / `run-action` /
   `summarize` / `alert …`). The SOAR-plane case **config** surfaces are config-as-code,
   not triage, and stay under `soar`: `soar pull case-tags|case-stages`,
   `soar push case-tags|case-stages|close-root-causes|bulk-close`, `soar users`.

3. **`entities` is canonical; there is no `entity`.** Singular `entity` is removed in
   favor of the plural collection name, consistent with every other collection group.

## The rename map

Earlier name → Phase D command. The earlier column collapses the pre-0.6.0 spellings
(including intermediate clarity-pass names) into the one final name.

### Search & queries

| Earlier | Phase D command |
|---|---|
| `query udm` | `search udm` |
| `query raw` | `search raw` |
| `query stats` | `search stats` |
| `query run` | `search run` |
| `query saved …` | `search saved …` (`list`/`get`/`run`/`save`/`share`/`unshare`/`delete`) |
| `query nl '<q>'` | `gemini search '<q>'` |
| `query nl --translate-only '<q>'` | `gemini generate '<q>'` |
| `query gemini '<q>'` | `gemini ask '<q>'` |

### Detection rules

`rules` is for the detections you **author**; Google-managed **predefined** detections
are `curated`. Cross-cutting concerns that span both sources are top-level, not nested
under `rules`: **`exclusions`** (findings refinements filter noise from custom *and*
curated detections) and **`mitre`** (ATT&CK coverage aggregates both). `health` stays
under `rules` (it rolls up the custom rules you control). The brief v0.6.0 nesting of
`curated`/`exclusions` under `rules` was reverted in v0.6.1.

| Earlier | Current command |
|---|---|
| `curated …` (briefly `rules curated …`) | `curated …` (`list`/`rules`/`rule`/`rule-sets`/`set`/`detections`/`events`/`trends`) |
| `rule-exclusions` / `rule_exclusions` (the command; briefly `rules exclusions`) | `exclusions` (`list`/`get`/`deploy`) |

### Threat intel & IOCs

| Earlier | Phase D command |
|---|---|
| `iocs` / `indicators` (find/get/related) | `ti find` / `ti get` / `ti related` |
| `threat-intel` (collections/collection) | `ti collections` / `ti collection` / `ti collection-matches` |

### Lists

| Earlier | Phase D command |
|---|---|
| `reference-lists` (the command, e.g. `empty`) | `lists` (`lists empty`) |
| `watchlists …` | `lists watchlists …` |

### SOAR

| Earlier | Phase D command |
|---|---|
| `soar marketplace …` | `content-hub …` (top-level: `browse`/`list`/`get`/`install`/`uninstall`/`contentpacks`) |
| `soar playbook …` | `soar playbooks …` |
| `soar integration …` | `soar integrations …` |
| `soar job …` | `soar jobs …` |
| `soar build-playbook` | `soar ide build-playbook` |
| `soar package-integration` | `soar ide package-integration` |
| `soar case …` (triage verbs) | `cases …` (top-level) |

### Status & diagnostics

| Earlier | Phase D command |
|---|---|
| `capabilities` | `status capabilities` |
| `coverage` | `status coverage` |
| `surfaces` | `status surfaces` |

### Ingestion

| Earlier | Phase D command |
|---|---|
| `feeds …` | `ingest feeds …` |
| `forwarders …` | `ingest forwarders …` |
| `parsers …` | `ingest parsers …` |
| `log-types …` | `ingest log-types …` |
| `pipeline …` | `ingest pipeline …` |
| `ingestion health` | `ingest health` |

### Entities

| Earlier | Phase D command |
|---|---|
| `entity …` | `entities …` (`summarize`/`graph`/`risk-scores`) |

## What does NOT rename

These spellings are deliberately unchanged:

- **`pull` / `push` target args and mirror directory names** — `reference_lists`,
  `rule_exclusions`, `feeds`, `parsers`, `curated`, `data_tables`, and the
  `rules-create` / `rules-update` / `rules-deploy` / `rules-disable` push verbs (see
  locked decision 1).
- **`drift`** stays top-level — it is the config-as-code loop's drift gate, not a
  diagnostic subcommand, so it did **not** fold into `status`.
- **`data-access`** stays top-level (labels + scopes RBAC), not under `ingest`.
- **Literal commands** that were already clear: `commands`, `info`, `doctor`,
  `config`, `version`, `pull`, `push`, `dashboards`, `soar`, `cases`, `alerts`,
  `entities`, `skill`, `cleanup`.
- **`soar legacy call`** — the raw passthrough escape hatch.
- **Go SDK method names** — the rename is the CLI surface only; the importable
  `chronicle/` and `soar/` packages keep their method names.

## Conventions for new commands

When adding a command later, follow the same rules so the surface stays coherent:

- Name it for the noun it manages; group verbs under that noun rather than adding a
  new top-level name.
- Use the plural for a collection and the hyphenated form for multi-word names.
- If the underlying record is already reachable by another command, extend that
  command (a verb or flag) rather than adding a second top-level name for it.
- Spell the host/plane in `--help`, never in the command name — the tool picks the
  host.
- Add no alias. One surface, one runnable name.

## Discovery & verification

`commands --json` is the source of truth for the surface: every runnable verb with
its `path` and `kind` (read vs guarded-mutation), offline, no live call. Group the
output by the first path segment to see the top-level map; `status surfaces` and
`status capabilities` describe the API families and per-surface state behind those
commands. Any doc that names a command path is checked against `commands --json` — a
path that is not in that catalog is a bug in the doc, not a hidden alias.
