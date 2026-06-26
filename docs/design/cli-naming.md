# Command naming & clarity (v0.5.1)

The v0.5.1 release is a **clarity pass over the command surface**: it makes every
top-level command name say what it operates on, and collapses the places where one
SecOps record was reachable through two differently-named commands. No new
capability is added or removed — the same verbs do the same work — but the names
and the command tree become self-describing, so an operator (or an agent reading
`commands --json`) can find the right command without prior knowledge.

It pairs with the [glossary](../GLOSSARY.md), which defines the underlying terms
(rule, curated rule, alert, case, playbook) and the *one concept, not two* rule.

## Principles

1. **Clear over terse.** A command name spells out the noun it manages. Acronyms
   that aren't universally known (`iocs`, `ti`) become words (`indicators`,
   `threat-intel`).
2. **One concept, one command.** A single SecOps record gets a single command,
   auto-routed to whichever API host serves it. The host/auth plane is an
   implementation detail, not something the operator selects by picking a command.
3. **Zero breakage.** Every rename keeps the previous name as a permanent hidden
   **alias**. Existing scripts, muscle memory, and the operator field report's
   documented invocations all keep working unchanged. Aliases are not deprecated
   and are not scheduled for removal.
4. **Consistency.** Collection commands are plural nouns (`alerts`, `cases`,
   `rules`, `watchlists` → also `entities`). Multi-word command names use hyphens,
   matching the cobra convention already used by every flag.

## Release scope

v0.5.1 lands as two roadmap waves:

- **Wave 85 — unify the case surface + fail-fast reads.** A case is one record;
  it becomes one command. See [ROADMAP Wave 85](../../ROADMAP.md).
- **Wave 86 — command naming clarity.** The renames below, all with aliases.

## The rename map

Every row keeps the old name working as a hidden alias.

| Today | v0.5.1 canonical | Alias(es) kept | Why |
|---|---|---|---|
| `iocs` | `indicators` | `iocs` | Spell out the acronym; `indicators find\|get\|related` reads as plain English. |
| `ti` | `threat-intel` | `ti` | Spell out the acronym; the command browses the threat-intelligence catalog. |
| `entity` | `entities` | `entity` | Plural, to match every other collection command. |
| `reference_lists` | `reference-lists` | `reference_lists` | Hyphen per cobra norm; underscore form stays valid. |
| `rule_exclusions` | `rule-exclusions` | `rule_exclusions` | Hyphen per cobra norm; underscore form stays valid. |
| `cases` (Chronicle alt) | `cases` (unified) | `soar case` | One record, one command — see Wave 85 below. |

Commands already clear are unchanged: `alerts`, `rules`, `curated`, `dashboards`,
`feeds`, `parsers`, `pipeline`, `watchlists`, `query`, `drift`, `cleanup`,
`doctor`, `info`, `pull`, `push`, `soar`.

### Pull/push target args are not renamed

`reference_lists` and `rule_exclusions` are also **target arguments** to `pull` and
`push` (`pull reference_lists`), where they name the on-disk mirror directory
(`./reference_lists/`). Those arg spellings stay snake_case so they keep matching
the directory names. Only the standalone **command** names gain the hyphenated
canonical form. Both spellings are accepted as the target arg for safety.

## The case unification (Wave 85)

A case is a single SecOps record reachable on two API hosts:

- the **SOAR** host (Siemplify, AppKey) — where every case verb works, today's
  `soar case …` tree (`list/get/assign/tag/close/merge/comment/alert/…`); and
- an alternate **Chronicle** host path (ADC) that addresses the same case by UUID
  but errors at every API version.

Today the Chronicle path is also surfaced as a top-level `cases` command whose
`list/get/search` verbs are dead duplicates of `soar case` (they 500/404), with one
working verb — `cases soar-id`, the SIEM-UUID → SOAR-integer-id bridge.

v0.5.1 collapses this:

1. **`cases` becomes the single top-level case command**, auto-routed to the SOAR
   host (the working path). All `soar case` verbs are reachable as `cases …`.
2. **`soar case` stays as a hidden alias** so existing invocations keep working.
3. **The dead Chronicle-host `cases list/get/search` verbs are removed**, along
   with the `cases (chronicle alt)` entry in the surface-family registry.
4. **`cases soar-id` stays** as the one Chronicle-host case read that answers — the
   UUID→id bridge (its SDK method `BatchGetCases` remains importable).

## Fail-fast reads

Folded into Wave 85 because the worst stall is a blocked case read:

1. **Per-request timeout (`--timeout`, default 60s, `0` disables).** Each
   individual API request gets a client-side deadline (`http.Client.Timeout`), so a
   slow or blocked endpoint fails fast instead of stalling. The timeout is
   per-**request**, deliberately not per-command: it never spans an interactive
   confirm prompt (so a deliberated `push` can't abort) and never caps a multi-call
   command (`pull all`, paginated reads) in aggregate — only a single request that
   itself runs long is cut. Raise it for one very large request.
2. **Default list cap.** `list` reads carry a default `--limit` with a `showing N
   of M (raise --limit)` note — the pattern `query udm` already uses — so a large
   result set can't silently dominate latency.

## How aliases are implemented

Cobra command aliases (`Aliases: []string{"iocs"}`) resolve the old name to the
renamed command with identical behavior. Aliases stay out of the primary help
listing (help shows the one canonical name) but remain fully runnable.

Discovery: a renamed leaf verb carries its alias in its `commands --json` row; the
renamed **groups** (`indicators`, `threat-intel`, …) are navigation-only and not
catalog rows, so their old→new mapping is exposed by `capabilities --json` under
`command_aliases` (old name → canonical name). The `commands` catalog stays a list
of runnable verbs only; an agent keys off the canonical `path`.

## Conventions for new commands

When adding a command later, follow the same rules so the surface stays coherent:

- Name it for the noun it manages; avoid acronyms unless universally known.
- Use the plural for a collection, the hyphenated form for multi-word names.
- If the underlying record is already reachable by another command, extend that
  command (a verb or flag) rather than adding a second top-level name for it.
- Spell the host/plane in `--help`, never in the command name — the tool picks the
  host.

## Test plan

- **Alias resolution.** A table test asserts each old name resolves to the renamed
  command and runs the same `RunE` (offline; no live call).
- **Help tree.** The existing tree-walking help guard is extended to assert every
  renamed command shows its canonical name in help and that aliases are hidden but
  runnable.
- **`commands --json` shape.** Assert each renamed entry carries both the canonical
  `path` and the `aliases` list.
- **Surface registry.** Assert the `cases (chronicle alt)` blocked entry is gone
  and the registry drift-guard still passes.
- **Gates.** `go build/vet/test`, `golangci-lint`, `check-lengths.sh`,
  `markdownlint` — all green before the release tag.
- **Live validation before tagging.** The pure renames are offline (alias
  resolution only), but the unified `cases` command and the fail-fast read defaults
  touch live hosts, so they are exercised against a live SecOps instance before the
  release is tagged — read paths first, any guarded mutation dry-run-first — to
  confirm the routing, timeout, and default-limit behavior match the prior path.

## Out of scope

- No verb-level renames inside a command tree (e.g. `soar case` sub-verbs keep
  their names); this pass is top-level command clarity only.
- No removal of any alias — back-compat is permanent.
- No behavior change to any command beyond the case routing and the fail-fast
  read defaults above.
