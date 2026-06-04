# 03 · YARA-L Detection Rules

Custom detection rules are written in **YARA-L 2.0** and tracked as code. This
doc covers the file layout, the read-only metadata contract, conventions for
writing rules, and the two push paths. For the surrounding loop see
[01-secops-as-code.md](01-secops-as-code.md); for how `etag` and slugs work see
[02-architecture-client.md](02-architecture-client.md).

## One `.yaral` + one companion `.yaml`

Each rule is two files sharing a slug stem:

```
rules/<slug>.yaral   # the YARA-L 2.0 rule text — this is what you edit
rules/<slug>.yaml    # metadata + deployment state — mostly written by pull
```

The `.yaral` is the source of truth for detection *logic*. The companion `.yaml`
carries:

```yaml
display_name: My Suspicious Login Rule
rule_id:      ru_xxxxxxxx-...     # server identity — READ-ONLY (written by pull)
name:         projects/.../rules/ru_...   # full resource path
etag:         "..."              # optimistic-concurrency token — READ-ONLY
type:         SINGLE_EVENT
severity:     High
allowed_run_frequencies: [LIVE, HOURLY, DAILY]
time_window_duration: 3600s
deployment:                       # live deployment state
  enabled:   true
  alerting:  true
  runFrequency: LIVE
```

**`rule_id` and `etag` are read-only** — they are populated by `pull` and you
never hand-write them. Touching them breaks round-tripping. Edit detection logic
in the `.yaral`; leave the companion `.yaml` alone except where you deliberately
intend a deployment-state change that a push subcommand reads.

## Writing a rule: conventions

- **Naming.** Adopt a single house convention for rule display names (a
  consistent prefix or PascalCase scheme) so custom rules are visually distinct
  from curated ones in the UI. Be consistent across the whole `rules/` directory.
- **Severity lives in `meta`.** Set severity in the YARA-L `meta` block, *not* in
  the companion YAML. The `severity` field in the YAML is a pull-time reflection
  of what the server parsed from the rule; the rule text is authoritative.
- **A useful `meta` block** typically carries `author`, `severity`, `priority`,
  `rule_owner`, `data_source`, and MITRE `mitre_tactic` / `mitre_technique`. These
  travel with the rule and show up in the UI and in alerts.
- **Identity matching** is most robust on a full, normalized field such as
  `principal.user.email_addresses` (a complete email) rather than a bare
  `userid` substring — fewer false matches, fewer surprises.
- **Filter on the field that actually carries the signal**, verified against live
  data. A rule that filters on a vendor tag, log type, or field your events do not
  populate compiles cleanly and then *silently never fires*. Validate the field
  shape with a UDM query first — see [07-udm-queries.md](07-udm-queries.md). A
  vendor-emitted severity level is usually a poor sole filter; the event-type /
  category field generally gives far better signal-to-noise.

### Single-event vs. aggregation rules

A rule with no `match` section evaluates one event at a time and can run `LIVE`
(immediate). A rule with a `match` (aggregation / threshold / correlation over a
window) generally cannot run `LIVE` — the API downgrades multi-event rules to
`HOURLY`. Pick the run frequency the rule actually supports; `allowedRunFrequencies`
in the companion YAML tells you what the server will accept.

A worked tuning lesson: a "fire on every CRITICAL vendor event" rule drowned in
background-scan noise, while splitting by event-type category (alert only on true
threat categories, demote informational categories to dashboards) turned the same
data into actionable signal. Aggregation (`N events from one source in M minutes`)
is the tool for taming bursty, low-value-per-event streams.

## Push path 1: create new rules

```
secopsctl push rules-create [--dry-run | --yes]
```

A rule is "new" when its `.yaral` exists with **no companion `.yaml`** — there is
no `rule_id` yet, so it cannot already exist server-side. `push rules-create`
finds every such orphan `.yaral`, calls `create_rule(text)`, then (optionally)
sets deployment state (`enabled` / `alerting` / `run_frequency`) via
`update_rule_deployment`. So to add a rule: write only the `.yaral` and push. The
companion `.yaml` materializes when you re-`pull`.

## Push path 2: disable tracked rules

```
secopsctl push rules-disable [--dry-run | --yes]
```

This disables every locally-tracked rule whose `deployment.enabled` is `true`,
calling `update_rule_deployment(rule_id, enabled=False)`. It is the fast "turn off
this noisy batch" path; `alerting` and run-frequency are preserved so re-enabling
restores prior behavior.

## Both push paths are guarded

`push` defaults to `--dry-run=True`. Each mutating subcommand prints a
`LIVE DEPLOY` banner and a dry-run preview of exactly what it will create or
disable, and refuses to mutate until you pass `--yes` (or answer an interactive
`y/N` prompt). Read the preview, confirm it is what you intend, then deploy.

Editing existing rule *text* is done by editing the `.yaral` and pushing through
your deploy flow; the `etag` in the companion YAML guards against clobbering a
concurrent UI edit (see [02-architecture-client.md](02-architecture-client.md)).
After any mutation, re-`pull rules` so the companion YAML reflects the new
`etag` and deployment state — remember, done = deployed *and* local mirrors live
([01-secops-as-code.md](01-secops-as-code.md)).

## Custom rules vs. curated rules

The rules in `rules/` are *your* detections, fully editable. Google also ships a
large catalog of **curated** rules you cannot edit and that toggle only at the
rule-set level. When the curated catalog has a gap, or a curated rule won't fire
on your data shape, you write a custom rule here to cover it. That relationship —
and why tracking the full curated catalog is worth it — is in
[05-curated-rules.md](05-curated-rules.md). Custom rules also lean on reference
lists and data tables for their lookups; see
[04-reference-lists-data-tables.md](04-reference-lists-data-tables.md).
