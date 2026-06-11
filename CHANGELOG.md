# Changelog

Notable changes per release. Earlier releases (v0.1.x – v0.2.x) carry their
notes in the signed tag messages.

## v0.3.0 — 2026-06-11

The operational release: the alert → case → rule triage loop, the AI-assist
layer, the playbook authoring palette, and agent-safety guardrails.

### Triage loop

- `alerts update` — guarded SIEM alert disposition (status / verdict /
  priority / comment), with id fan-out.
- Alert ⇄ case bridges: `alerts get` resolves the SOAR case id;
  `cases soar-id` bulk-resolves SIEM case UUIDs.
- `soar case list` triage filters (`--assignee` / `--priority` / `--tag` /
  `--since`) plus a verbatim server-side `--filter`; the full modern filter
  grammar (scalars, `any()` collections, epoch-ms ranges) is documented and
  supported end to end — long filters auto-switch to the method-override
  POST, zero-match queries decode as empty results.
- `soar case counts` — per-priority queue numbers via the list's `totalSize`.
- Per-alert verbs inside a case: `soar case alert close | priority | move |
  reopen`; case verbs gain `priority`, `reopen`, `comment add/list`.
- Rule tuning reads: `rules trends | counts | events` and `curated
  detections | trends | events` (plus a gated batch rule update in the SDK).

### AI assist

- `alerts investigate <id> [--latest]` — the per-alert Gemini investigation:
  verdict, confidence, summary, suggested next steps (with the agent's UDM
  queries under `--json`); `--latest` is the read-only variant.
- `soar case summarize` — the structured Gemini case summary (poll-first;
  `--refresh` to regenerate).
- `query gemini` — environment-grounded SecOps Gemini chat.
- `soar playbook generate` — AI playbook drafting (returns the draft without
  persisting; instances may restrict the Playbook Assistant to interactive
  auth, surfaced plainly).

### Playbooks

- The authoring palette: `soar playbook components actions` (every action
  across every integration, with numeric ids), `flow` (transformers +
  logical operators), `triggers` (the trigger vocabulary), `blocks`;
  `components usage` resolves an action by name for impact analysis.
- Lifecycle completion: `versions`/`restore`, `stats`, `export`/`import`,
  `step skip`, batch `delete`, and job-instance schedule management.
- Custom Python definitions over the API: `soar integration action | job-def
  template | create | delete` (the IDE's create flow, guarded).

### Agent safety

- Hard read-only mode: `--read-only` / `SECOPS_READONLY=1` degrades every
  guarded mutation to a dry-run and refuses AI generations.
- `secopsctl commands` — the offline, machine-readable verb catalog
  (read vs guarded-mutation), for building automation allowlists. Each row now
  also reports per-command `--json` support (the `json` field / `JSON` column),
  so agents read it from the catalog instead of a hand-maintained doc list.
- A local JSONL audit log of confirmed mutations (`$SECOPSCTL_HOME/audit.jsonl`).

### SOAR administration

- `soar settings api-keys create | revoke` alongside `list` — the key value
  is minted locally (crypto/rand), shown exactly once, never logged.

### Fixes

- Watchlist entity writes send the UDM Entity envelope (the previous flat
  noun was rejected); `RemoveWatchlistEntity` removes by resource name.
- AI case summaries no longer restart generation on every read (poll-first).
- int64-bearing SOAR bodies (step skip, job instances, definition authoring)
  round-trip through raw-JSON overlays, immune to float64 truncation.

New guides: [triage](docs/guides/triage.md) ·
[playbooks](docs/guides/playbooks.md).
