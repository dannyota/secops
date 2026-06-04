# 09 · SOAR Operations

Google SecOps pairs the Chronicle **SIEM** with **Chronicle SOAR** (the product
formerly known as Siemplify — Google acquired it and rebranded; the API still
carries the older URL pattern). SOAR is where alerts become **cases**, where
**playbooks** automate response, and where **connectors** and **jobs** move data
in and out. This doc is conceptual: the two API surfaces, the playbook-versioning
trap, and the pattern of running case hygiene as detection-as-code on cron. It
deliberately contains no tenant specifics.

> SOAR is a **separate API from the SIEM.** Different host, different auth. The
> SIEM uses `Authorization: Bearer <ADC token>` against the Chronicle API host;
> SOAR uses a long-lived API key header against a per-tenant SOAR host. Keep that
> key out of the repo — in an env var or secret store, never in committed config
> (same rule as feeds, [08-feeds-parsers.md](08-feeds-parsers.md)).

## Two API surfaces

SOAR exposes **two** API surfaces on the same host, and the same API key works
for both. You pick per task — they don't share response shapes, so don't mix
them in one parser.

| Surface | Style | Best for |
|---|---|---|
| **Legacy** (`/api/external/v1/*`) | RPC-ish paths, payload-style responses | Listing, closing, and **bulk** case operations; playbook export/import/delete. |
| **Modern v1alpha** (Google-style resource paths) | `projects/.../locations/.../instances/...`, `{items: [...]}` responses, `updateMask` sparse updates | Integrations and connectors discovery, connector/job *instance* config (schedules, parameters, filters), alert-grouping rules, module settings, and the playbook read/save bridge. |

Rule of thumb: **bulk case operations live on legacy**; **resource configuration
(connectors, jobs, grouping, module settings) lives on v1alpha**. When in doubt,
list on both and see which returns the richer record.

### Two case ID systems (gotcha)

The same conceptual case has two identifiers: an **integer ID** used by the SOAR
case endpoints and shown in the UI URL, and a **UUID** used by the
SIEM-side/legacy case lookups. They are not interchangeable. Map between them via
the lookup that returns both (the SIEM-side case fetch exposes the SOAR integer
ID alongside the UUID). Always know which ID a given endpoint expects.

## The playbook-versioning gotcha

This is the SOAR trap that bites everyone:

> **A playbook is version-scoped. Every save mints a *new* identifier, and the
> identifier you just saved is now stale.** Never cache a playbook UUID across a
> save.

Consequences and the safe pattern:

- After any save, **re-resolve the playbook by display name** (or by a stable
  lineage anchor the API exposes), never by the UUID you held before the save.
- A save **replaces the whole definition** — it is not a sparse patch. To change
  one thing (a trigger condition, a step), you must **read the current full
  body → modify it → save it whole.** Trigger and step edits persist only if they
  ride along in that whole-body save.
- Some UUIDs are *readable but un-saveable* — stale orphan versions left from
  prior edits, not present in the list view. If a save is rejected as
  un-saveable, you're holding an orphan; re-resolve by name.
- **Triggers fire only on new cases.** To run a playbook against an existing or
  historical case, use the explicit attach-and-run operation rather than waiting
  for a trigger.

There are also naming constraints (SOAR rejects various punctuation in playbook
names — stick to letters, digits, spaces, hyphens, underscores) and step-shape
rules for integration-action steps. Treat playbook automation as something you
prototype in the UI, then export and check into git, rather than hand-authoring
blind.

## Case hygiene as detection-as-code

A busy SOAR queue fills with low-value and stale cases. Rather than build native
SOAR playbooks for housekeeping, a clean pattern is to run **case-hygiene logic
as code on a schedule** — the same detection-as-code philosophy as
[01-secops-as-code.md](01-secops-as-code.md). The logic lives in your repo,
changes go through `git diff` review, and a cron host (or timer / CI schedule)
runs it. Typical safe loops:

- **Close stale low-priority cases** — e.g. cases below a priority threshold,
  still in an early triage stage, untouched for N days.
- **Close confirmed false positives** — e.g. cases an analysis layer (or an LLM
  triage step) marked high-confidence false-positive with no true-positive
  signal, under a priority ceiling.
- **Promote named-threat-actor cases** — bump cases whose titles match a
  threat-actor naming pattern up to at least a review-worthy priority. Idempotent:
  a re-run is a no-op once everything is at or above the target.

Two design rules make these safe to automate:

1. **A high-value denylist that nothing auto-closes.** Maintain a list of title
   patterns — beaconing, named APTs, RATs, reverse shells, "multiple MITRE
   tactics," etc. — that are **never** auto-closed, even when age or a
   false-positive verdict says "noise." A case can be low priority simply because
   nobody triaged it yet; a future real detection in those categories must
   survive until a human looks. An automated verdict reflects *yesterday's*
   behavior — it is a strong second opinion, not a replacement for analyst review
   on named-actor surfaces.
2. **Dry-run first; ceilings, not blanket action.** Run the preview when you
   change patterns, cap priority/age thresholds, and chunk bulk closes. These
   standing jobs are the *only* pre-authorized recurring pushes — everything else
   still goes through the review gate. See
   [10-llm-and-automation.md](10-llm-and-automation.md) for the broader
   dry-run-first automation pattern.

## What's worth tracking

Export and version the SOAR state that matters for review and disaster recovery:
playbooks (exported definitions), an open-case snapshot, scheduled **jobs**, and
ingestion **connectors** (including their instance config — schedules,
parameters, and any allow-list/filter expressions). As with every other entity,
the file is the reviewable artifact; the live instance is the thing you actually
deploy to.
