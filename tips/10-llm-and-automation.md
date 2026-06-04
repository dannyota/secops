# 10 · LLM Agents & Automation

`secopsctl` is built so that an **LLM agent** can drive it as safely as a human
— and so that the safe, repetitive parts of SecOps operations can run
unattended. This doc covers how an agent should call the CLI, and the
detection-as-code automation patterns that keep unattended work reviewable.

It builds on the loop in [01-secops-as-code.md](01-secops-as-code.md) and the
client design in [02-architecture-client.md](02-architecture-client.md).

## Why this CLI is agent-friendly

The design choices that make the tool predictable for humans are exactly what an
agent needs:

- **Deterministic flags, no hidden interactivity.** Every action is a flag, not a
  menu. The *only* interactive prompt is the push confirmation, and it is
  skippable with `--yes`. An agent never gets stuck waiting on a prompt it can't
  see.
- **`--json` output.** Pass `--json` (e.g. on `query udm`) to get
  machine-readable results instead of human formatting. Parse the JSON; don't
  scrape the pretty output.
- **Clear `--help` on every command.** An agent can introspect the command
  surface (`secopsctl --help`, `secopsctl pull --help`, …) to discover targets
  and flags rather than guessing.
- **Lazy imports / offline-safe.** The package imports and `--help` works without
  the heavy SDK installed, so an agent can explore the surface before any network
  or auth is in play.
- **Read/write asymmetry is explicit.** `pull` and `query` never mutate the
  instance; only `push` does, and it tells you so loudly.

## The dry-run-first contract for agents

An agent should treat every mutation as a two-phase operation:

1. **Preview.** Run the mutating command in its default `--dry-run` mode. The
   tool prints a `LIVE DEPLOY` banner and a preview of exactly what *would*
   change — which rules get created, which get disabled, which cases would close.
2. **Decide, then deploy.** Parse the preview. If — and only if — it matches the
   intended change, re-run with `--yes`. If it touches anything unexpected,
   **stop and surface the preview** to a human instead of deploying.

```bash
secopsctl push rules-create            # dry-run: preview what would be created
# (agent inspects the preview)
secopsctl push rules-create --yes      # deploy, only after the preview checks out
```

Suggested guardrails for an autonomous agent:

- **Reads parallelize; writes serialize.** Multiple agents can `pull`/`query`
  concurrently (reads never touch tenant state). Never run two `push` operations
  against one instance at once, and never let two agents edit the same entity
  area without a fresh `pull` each ([01-secops-as-code.md](01-secops-as-code.md)).
- **Pull before edit; re-pull after deploy.** Refresh local state before editing,
  and re-pull after any mutation so companion metadata (`etag`, server IDs,
  deployment state) mirrors live. Don't infer "is X live?" from git — verify
  against the instance.
- **Never invent identifiers.** Read project/region/customer from config
  ([02-architecture-client.md](02-architecture-client.md)); never hard-code a
  tenant value into a command.
- **Escalate, don't improvise.** If a dry-run preview is surprising, an `etag`
  mismatch appears, or a feed is `FAILED`
  ([08-feeds-parsers.md](08-feeds-parsers.md)), report it rather than forcing
  through.

## Verify before you trust (especially as an agent)

The lesson in [07-udm-queries.md](07-udm-queries.md) is doubly important for an
agent, which is prone to reasoning from a rule's *name* rather than its behavior:
**before relying on any curated/third-party rule that filters on a vendor tag,
run a UDM query and confirm your events actually carry that
`vendor_name`/`log_type`.** A rule whose vendor filter never matches is silently
dead. An agent that "verifies enablement" by reading the dashboard will report
green while detection coverage is zero. Verify against data, not against config.

## Detection-as-code automation patterns

The unattended work that pays off is the **safe read/close loops** — bounded,
idempotent, reviewable. Keep the logic in the repo so changes go through
`git diff`, and run it on a schedule (cron, a systemd timer, or a CI schedule).

Good candidates to automate:

- **Ingest-health checks** — pull feeds on a schedule and alert on any
  `state: FAILED` ([08-feeds-parsers.md](08-feeds-parsers.md)). Read-only; safe to
  run often.
- **Case hygiene** — close stale low-priority cases, close confirmed
  false-positives, promote named-actor cases
  ([09-soar-operations.md](09-soar-operations.md)). These are *mutating*, so they
  earn their keep only with the two safety rules below.

Two rules make a mutating loop safe to leave running:

1. **A high-value denylist that nothing auto-closes** — beaconing, named APTs,
   RATs, reverse shells, multi-tactic alerts. A future real detection in those
   categories must survive any auto-close until a human looks. An automated
   (or LLM) verdict reflects past behavior; it is a second opinion, not a
   substitute for analyst review on those surfaces.
2. **Bounded and idempotent** — priority/age ceilings, chunked bulk operations,
   re-runs that are no-ops once the desired state holds. Always dry-run first when
   you change the patterns. These standing jobs are the only pre-authorized
   recurring pushes; everything else still goes through the review gate.

## Review in git; cron the safe loops

The throughline: **everything reviewable lives in git, and only the proven-safe
read/close loops run unattended.** An agent (or a human) proposes a change as a
diff, the diff is reviewed, and it deploys via a dry-run-then-`--yes` push.
Recurring automation is narrow, denylisted, idempotent, and scheduled from the
repo so its logic is itself code-reviewed. New, novel, or destructive changes are
never left to a schedule or an autonomous agent — they go through the human review
gate every time.

Cross-references: the loop and discipline
([01-secops-as-code.md](01-secops-as-code.md)), config-as-identity and `etag`
([02-architecture-client.md](02-architecture-client.md)), rule mutation specifics
([03-yara-l-rules.md](03-yara-l-rules.md)), the vendor-tag verification lesson
([07-udm-queries.md](07-udm-queries.md)), ingest health
([08-feeds-parsers.md](08-feeds-parsers.md)), and SOAR case hygiene
([09-soar-operations.md](09-soar-operations.md)).
