---
name: secopsctl
description: >
  Operating guide for AI agents driving the secopsctl CLI against a Google
  SecOps instance (Chronicle SIEM + Siemplify SOAR). Encodes the two-auth-plane
  model, the mutation ritual, the config-as-code loop, self-discovery commands,
  and the gotchas the per-command --help can't express. Read this before issuing
  any secopsctl command.
---

# secopsctl agent operating guide

## Session bootstrap — do these first

```bash
secopsctl doctor           # config + auth + SIEM/SOAR reachability (read-only)
secopsctl capabilities --json   # Wave 73+: version, per-surface status, auth health per plane, read-only state
secopsctl commands --json  # every verb: path, kind (read/guarded-mutation), flags, --json support
```

`doctor` is the gate: if either plane reports unhealthy, fix auth before proceeding.

`capabilities --json` and `commands --json` are the **live source of truth** for what the
binary supports. Prefer them over any static list — surfaces and flags evolve with each release.

## The two auth planes

Every secopsctl command runs against one of two independent credential planes.

| Plane | Host | Auth | Commands |
|---|---|---|---|
| **SIEM** (Chronicle) | `{region}-chronicle.googleapis.com` | Google ADC / OAuth token (minted in-process, never on disk) | `pull`, `push`, `drift`, `rules`, `alerts`, `curated`, `query`, `entity`, `iocs`, `ti`, `feeds`, `parsers`, `dashboards`, `watchlists`, `cases`, `pipeline` |
| **SOAR** (Siemplify) | `{tenant}.siemplify-soar.com` | AppKey (`soar_app_key` in config or `$SECOPS_SOAR_APP_KEY`; no ADC) | `soar pull`, `soar push`, `soar case`, `soar playbook`, `soar job`, `soar integration`, `soar marketplace`, `soar settings`, `soar legacy` |

The planes are **independent**: a SOAR AppKey call works even when SIEM ADC is expired.

### SIEM auth recovery

The ADC token carries a short-lived RAPT (reauth proof token). When a SIEM call fails with
`invalid_grant` or `"reauth … invalid_rapt"`:

1. Re-auth: `gcloud auth login` or `gcloud auth application-default login`.
2. Then mint a fresh token **in the same shell**: `SECOPS_ACCESS_TOKEN=$(gcloud auth print-access-token) secopsctl ...`
3. Confirm recovery: `secopsctl doctor`.

SOAR commands are **unaffected** by an ADC lapse — continue SOAR work while SIEM re-auth proceeds.

### Config resolution (highest priority first)

`SECOPS_*` env vars → `--config <path>` / `$SECOPSCTL_CONFIG` → `~/.secopsctl/instance.yaml`
→ `./config/instance.yaml` → `~/.config/secopsctl/instance.yaml`.

Verify the active config with `secopsctl info` (AppKey is redacted).

## The mutation ritual — every guarded verb

`pull` is always read-only. Every verb that changes live state is **guarded**:

1. Run with no extra flags — it defaults to `--dry-run` and prints a preview with a `LIVE DEPLOY` banner.
2. Read the preview. Verify what will change.
3. Pass `--yes` to apply.

```bash
# Correct pattern for any guarded verb:
secopsctl push rules-update               # step 1: dry-run preview
secopsctl push rules-update --yes         # step 3: live deploy

secopsctl soar push webhooks --dry-run    # explicit dry-run (same as default)
secopsctl soar push webhooks --yes        # apply
```

**Never skip the preview.** A `push` is a production deploy to a live SIEM or SOAR instance.

### Hard read-only mode for automation

Set `SECOPS_READONLY=1` in the environment (or pass `--read-only`) before launching an
autonomous agent. Every guarded mutation degrades to a dry-run preview even with `--yes`.
AI generations that create server-side artifacts are refused outright. Every confirmed
mutation and read-only refusal appends one JSONL record to `~/.secopsctl/audit.jsonl`.

### --prune — delete server-only objects

`push <target> --prune` deletes objects on the server that have no local file. It is
**not enabled by default** and requires an explicit pull this session (the reconciler gates on
a fresh pull state). Run `push <target> --help` to see whether a surface supports `--prune`.

## The config-as-code loop

```text
pull live state  →  review in git diff  →  push back
```

```bash
secopsctl pull rules                  # snapshot to local files
git diff                              # the review surface
secopsctl push rules-update --dry-run # preview
secopsctl push rules-update --yes     # deploy
secopsctl pull rules                  # re-pull so local matches live
```

Git history is the source of truth. **Always pull before editing** — live UI edits happen
out-of-band and a stale local state silently clobbers them on push.

The same loop applies to every reconcile surface: `reference_lists`, `data_tables`, `feeds`,
`parsers`, `dashboards`, `soar/webhooks`, `soar/playbooks`, `soar/connectors`, `soar/jobs`, etc.

Surfaces and their lane (reconcile / imperative / operational) are listed by `secopsctl surfaces`.

## Command self-discovery

Do not guess command names, flags, or enums. Use the live catalog:

- `secopsctl commands --json` — every verb: `path`, `kind`, `flags`, `json` (whether `--json` is supported).
- `secopsctl capabilities --json` — (Wave 73+) version, auth health per plane, per-surface status, read-only state.
- `secopsctl surfaces [--json]` — every API surface family: plane, version, lane, status, `--prune` eligibility.
- `<cmd> --help` — per-command flags, including a per-target note on plane/version/write gotchas.

Filter `commands --json` to build an allowlist: `kind == "read"` entries are always safe;
`kind == "guarded-mutation"` entries require the dry-run → `--yes` ritual.

## Gotchas

### A 500 is usually wrong-host, not a broken endpoint

Two hosts serve the same v1alpha API paths — `chronicle.googleapis.com` (ADC, SIEM surfaces)
and `{tenant}.siemplify-soar.com` (AppKey, SOAR surfaces). SOAR-flavored surfaces
(cases, Content Hub, connectors) **500 on the chronicle host** and work on the SOAR host.
SIEM surfaces (rules, iocs, riskConfig) 404 on the SOAR host.

Before declaring a surface broken, try `--legacy` (forces the legacy AppKey path on
dual-generation surfaces) or `secopsctl soar legacy call` as an escape hatch.

### Curated rules: toggle is set×precision only

`curated set` toggles `enabled`/`alerting` scoped to a `--category`, `--ruleset`, or
`--precision`. There is no per-rule override for Google-managed curated rules — that is a
platform limit, not a CLI limitation.

### Playbook UUIDs rotate on save

Every save of a SOAR playbook mints a new `identifier` (UUID). Code that resolves a
playbook by identifier will break after any edit. **Always resolve by name** (`--name`)
rather than by identifier, and re-read the list after a save.

### Pull-time secret redaction

`pull` masks values matching patterns in a `.secopsctl-redact` file at the data root (or an
ad-hoc `soar pull --redact <regex>`), replacing each match with the literal `***REDACTED***`.
The masking is drift-safe: pull, drift, and push all load the same patterns, so a masked
value canonicalizes identically on every side and never produces a phantom diff. A push of a
body still carrying the `***REDACTED***` marker is **refused** — restore the real value, or
reference a SOAR credential / env var, before pushing. Never hand-edit the marker into a body.

### Write-then-list has indexing lag; a failed write may have applied

After any create/update call:

- **A write can return an error yet still persist the object** (create-despite-error). After a
  failed write, verify with a get/list — do not assume the error means nothing happened.
- **Delete by exact id, not a list sweep** — create→list has indexing lag and deleted ids
  tombstone. Give throwaway objects unique, self-identifying names (e.g. `secopsctl-smoke-<nanos>`).

## Safety rules

- **No mutation without the dry-run review.** Show the preview, then pass `--yes`.
- **Never commit real identifiers.** Use placeholders (`your-project-id`, `000000000000`,
  `00000000-0000-0000-0000-000000000000`, `your-tenant`, `example.com`). The pre-commit
  hook at `.githooks/pre-commit` enforces this.
- **No secrets in the repo.** Config lives in git-ignored `~/.secopsctl/instance.yaml` (`0600`).
  Tokens are never written to disk. Do not commit AppKeys, OAuth tokens, or service-account JSON.
- **Clean up throwaways.** Delete by exact id any object created for a smoke test or probe.
  Use `secopsctl cleanup smoke-artifacts` for secopsctl-owned smoke objects.
- **Done = deployed live AND local mirrors live.** Re-pull the affected surface after any
  successful push so local files match the instance.

## Quick reference

| I want to… | Command |
|---|---|
| Verify setup | `secopsctl doctor` |
| Inspect resolved config | `secopsctl info` |
| Discover all commands | `secopsctl commands [--json]` |
| Inspect surface map | `secopsctl surfaces [--json]` |
| Snapshot SIEM surface | `secopsctl pull <target>` |
| Snapshot SOAR surface | `secopsctl soar pull <target>` |
| Deploy SIEM changes | `secopsctl push <target> --dry-run` → `--yes` |
| Deploy SOAR changes | `secopsctl soar push <surface> --dry-run` → `--yes` |
| List open cases | `secopsctl soar case list` |
| Triage a case | `secopsctl soar case get <id>` |
| Close a case | `secopsctl soar case close --id <n> --reason <r> --yes` |
| Ad-hoc UDM search | `secopsctl query udm '<filter>' [--hours N] [--json]` |
| Recover from ADC lapse | `gcloud auth login` then `secopsctl doctor` |
| Force legacy SOAR path | add `--legacy` |
| Hard read-only for agent | `SECOPS_READONLY=1 secopsctl ...` |

## Further reading

- `docs/guides/the-loop.md` — the pull → diff → push walkthrough in full
- `docs/guides/triage.md` — the SOC triage loop (queue → case → AI verdict → act → tune)
- `docs/guides/playbooks.md` — discover, author, operate SOAR playbooks
- `docs/guides/usage.md` — complete command reference with every flag
- `docs/guides/soar-cases.md` — per-case and per-alert verb reference
- `docs/design/catalog.md` — live status of every surface (designed / built / validated)
- `docs/design/surfaces.md` — API surface map by plane
- `docs/tips/10-llm-and-automation.md` — agent allowlists, audit log, full automation recipe
