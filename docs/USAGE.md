# secopsctl in practice — task cookbook

Copy-pasteable, end-to-end workflows for operating your SecOps tenant with
`secopsctl`. Each recipe has a one-line **why**, then the exact commands.

For the mental model see the [project README](../README.md); for *what each
surface supports* see [CATALOG.md](CATALOG.md); for unfamiliar terms see the
[Glossary](GLOSSARY.md). This page is just: **how do I get a job done.**

> Every command here uses placeholders — `your-project-id`, `000000000000`,
> `00000000-0000-0000-0000-000000000000`, `example.com`. Substitute your own.

---

## 0. One-time setup

**Why:** SIEM and SOAR are two surfaces with two credentials. Get both wired,
then prove they reach the tenant before you touch anything.

```bash
# 1. SIEM auth — Google ADC, nothing written to disk.
gcloud auth application-default login
#    No gcloud? Use a service account instead:
#    export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json
#    ...or a short-lived token: export SECOPS_ACCESS_TOKEN=ya29....

# 2. Write the config (project/region/customer + optional SOAR url/AppKey).
#    Single-screen form; the AppKey prompt is hidden. Writes
#    ~/.secopsctl/instance.yaml (0600), which is git-ignored.
secopsctl config

# 3. Prove it. doctor makes read-only calls to SIEM and (if soar_url is set)
#    SOAR. It mutates nothing.
secopsctl doctor

# Confirm what got resolved (AppKey is redacted; no API call):
secopsctl info
```

Notes:
- **SIEM commands** (`pull`, `push`, `query`, `doctor`, `curated`, `rules`,
  `ti`, `watchlists`) need ADC. If they fail with an auth error, re-run step 1.
  (`cases` is also a SIEM/ADC command, but it reaches a case on the chronicle
  host by UUID — a path that currently 500s, so for case work use `soar case`,
  see §3.)
- **SOAR commands** (`soar …`) need `soar_url` **and** an AppKey — no ADC.
  Generate the AppKey once in the SOAR UI (**Settings → Advanced → API Keys →
  Add**). `doctor` reports SIEM and SOAR independently, so a SIEM-only or
  SOAR-only setup is fine.
- Config resolves env vars first, then `--config`, then
  `~/.secopsctl/instance.yaml`. secopsctl does **not** read `.env`.

---

## 1. The golden rule: dry-run, then `--yes`

**Why:** every `push` and every `soar case` mutation is a **live production
deploy**. Reads are free; writes are guarded.

- **Reads** (`pull`, `query`, `info`, `doctor`, every `list`/`get`/`search`) never
  change anything.
- **Writes** default to `--dry-run` and print a `LIVE DEPLOY` banner. Nothing
  happens until you pass `--yes`.

The pattern, everywhere:

```bash
secopsctl push <target>            # dry-run by default — read the preview
secopsctl push <target> --yes      # apply for real
```

---

## 2. Edit a detection rule (config-as-code loop)

**Why:** keep your YARA-L in git, review every change as a diff, deploy with one
guarded command.

### Where files land

`pull rules` mirrors live rules into the current directory:

```
./rules/<slug>.yaral     # the YARA-L source — edit this
./rules/<slug>.yaml      # companion: server RuleID + etag + deployment state
```

The companion `.yaml` is the link to the live rule. **Don't delete or hand-edit
the RuleID/etag** — they drive the update and the optimistic-concurrency check.

- A `.yaral` **with** a companion `.yaml` = a tracked rule → `rules-update` /
  `rules-deploy`.
- A bare `.yaral` with **no** companion = a new rule → `rules-create`.

### Change the logic of an existing rule

```bash
# 1. Pull current state (always pull before you edit — UI edits happen too).
secopsctl pull rules

# 2. Edit ./rules/<slug>.yaral in your editor. Review the change:
git diff rules/

# 3. Preview the deploy (etag-guarded; refuses if the rule changed under you).
secopsctl push rules-update --dry-run

# 4. Deploy.
secopsctl push rules-update --yes

# 5. Re-pull so local matches live (refreshes the etag).
secopsctl pull rules
```

### Create a brand-new rule

```bash
# Drop a new file with NO companion yaml, e.g. ./rules/my_new_rule.yaral
secopsctl push rules-create --dry-run
secopsctl push rules-create --yes
secopsctl pull rules          # now it has a companion yaml + RuleID
```

### Turn rules on/off or change alerting

```bash
# Edit deployment.enabled / alerting / runFrequency in the companion .yaml, then:
secopsctl push rules-deploy --dry-run
secopsctl push rules-deploy --yes

# Quick path to disable every locally-enabled rule:
secopsctl push rules-disable --dry-run
secopsctl push rules-disable --yes
```

### Toggle a Google-managed (curated) rule set

**Why:** you can't author curated rules, only enable/alert them per precision.

```bash
secopsctl curated list
secopsctl curated set --category C --ruleset R --precision broad --enabled --dry-run
secopsctl curated set --category C --ruleset R --precision broad --enabled --yes
```

---

## 3. Triage a case

**Why:** this is the analyst loop — find the case, read it, act on it. Cases are
**not** reconciled from a file; you act per-case, guarded.

> **Two ids, one case.** For day-to-day triage use **`soar case`** — it runs on
> the siemplify domain (the New API v1alpha cases list, with auto-fallback to the
> Legacy AppKey queue). Its `--id` is the **SOAR integer id** shown by
> `soar case list` (e.g. `1234`), **not** the SIEM UUID. The separate `cases`
> command reaches the *same* case by its Chronicle UUID on the chronicle domain,
> but that path 500s at every version — prefer `soar case`.

```bash
# 1. List open cases (compact table; reads only, no banner).
secopsctl soar case list
secopsctl soar case list --status all --limit 50

# 2. Read one case + its alerts (use the integer id from the list).
secopsctl soar case get 1234

# 3. Act — every verb is dry-run first, then --yes.
secopsctl soar case assign --id 1234 --user <userId> --yes
secopsctl soar case tag    --id 1234 --tag triaged --yes
secopsctl soar case stage  --id 1234 --stage Investigation --yes

# 4. Close it. --reason is a string; --root-cause / --comment are optional.
secopsctl soar case close --id 1234 --reason "Malicious" \
    --root-cause "Phishing" --comment "Confirmed credential theft" --dry-run
secopsctl soar case close --id 1234 --reason "Malicious" --yes
```

Notes on the values:
- **`--user`** is a SOAR user id, or a role written as `@RoleName` (e.g. `@Tier1`).
- **`--reason`** on single-case `close` is a **free string**. The separate
  `bulk-close` (below) takes a **fixed enum** instead — they are not the same
  vocabulary.

### Close a batch of cases at once

**Why:** clearing a queue. Note `bulk-close` takes a **fixed reason vocabulary**,
unlike single-case `close`.

```bash
# reason ∈ malicious | not-malicious | maintenance | inconclusive | unknown
secopsctl soar push bulk-close --ids 1234,1235,1236 \
    --reason not-malicious --comment "False positive sweep" --dry-run
secopsctl soar push bulk-close --ids 1234,1235,1236 --reason not-malicious --yes
```

---

## 4. Reconcile a SOAR surface (config-as-code, with `--prune`)

**Why:** treat SOAR config (webhooks, connectors, jobs, environments, case
stages/tags, SLAs, …) like rules: pull → diff → push, one engine.

Files land under `./soar/<surface>/`, one redacted, diff-friendly file per object.

```bash
# 1. Snapshot the surface (read-only).
secopsctl soar pull webhooks

# 2. Edit files under ./soar/webhooks/, then review.
git diff soar/webhooks/

# 3. Preview create/update. Additive by default — never deletes.
secopsctl soar push webhooks --dry-run
secopsctl soar push webhooks --yes
```

### Deleting server-only objects (`--prune`) — handle with care

**Why:** `--prune` deletes live objects that have **no** local file. It is gated
on a complete pull, so the order matters.

```bash
# 1. Pull the WHOLE surface first (prune is refused on a partial snapshot).
secopsctl soar pull webhooks

# 2. Preview deletions — read the list carefully.
secopsctl soar push webhooks --prune --dry-run

# 3. Only then apply.
secopsctl soar push webhooks --prune --yes
```

Available surfaces: `blacklists`, `case-stages`, `case-tags`,
`close-root-causes`, `connectors`, `environments`, `idp`, `jobs`, `networks`,
`playbook-categories`, `playbooks`, `sla-definitions`, `soc-roles`,
`tracking-lists`, `visual-families`, `webhooks`. `soar pull all` snapshots
every one.

---

## 5. Ad-hoc event search (UDM)

**Why:** point-in-time hunting straight from the shell.

```bash
# Last 24h (the default window).
secopsctl query udm 'metadata.event_type = "USER_LOGIN"'

# Custom window + cap, machine-readable for piping.
secopsctl query udm 'metadata.event_type = "USER_LOGIN" and security_result.action = "BLOCK"' \
    --from 2026-06-01T00:00:00Z --to 2026-06-02T00:00:00Z --limit 500 --json

# Reuse a shipped example filter (the .udm files are commented templates —
# copy the filter line):
secopsctl query udm "$(grep -v '^#' examples/queries/<example>.udm)" --hours 48
```

---

## 6. Browse Threat Intel & the Content Hub (read-only)

**Why:** see the Mandiant threat intelligence your tenant is matched against, and
the marketplace content available.

```bash
# Threat collections (campaigns / reports), SIEM side.
secopsctl ti collections --limit 20
secopsctl ti collection <report-id>

# Content Hub (SOAR side) — installable integration packs and content packs.
secopsctl soar marketplace list --installed
secopsctl soar marketplace contentpacks
```

---

## 7. Scripting and the reliable path

**Why:** automate, and route around flaky APIs.

- **`--json`** — most reads emit machine-readable JSON for piping. Output shape
  is per-command.
  ```bash
  secopsctl soar case list --json | jq '.[] | {name, priority, status}'
  secopsctl pull rules >/dev/null && git diff --stat rules/
  ```
- **`--legacy`** — some New-API surfaces return HTTP 500 intermittently. For
  functions served by both generations (e.g. `soar case list`), `--legacy` forces
  the reliable Legacy (Siemplify external `/api/external/v1`, AppKey) path and
  skips the New API. Reach for it when a New-API call 500s.
  ```bash
  secopsctl soar case list --legacy
  ```
- **Escape hatch** — call any Siemplify external-API op directly:
  ```bash
  secopsctl soar legacy call integrations/GetInstalledIntegrations
  ```

---

## Quick reference

| I want to… | Command |
|---|---|
| Check resolved config | `secopsctl info` |
| Prove connectivity | `secopsctl doctor` |
| Pull SIEM config | `secopsctl pull <rules\|reference_lists\|…\|all>` |
| Edit a rule | edit `rules/<slug>.yaral` → `push rules-update --dry-run` → `--yes` |
| New rule | bare `.yaral` → `push rules-create --dry-run` → `--yes` |
| Hunt events | `secopsctl query udm '<filter>' --hours N` |
| List cases | `secopsctl soar case list` |
| Read a case | `secopsctl soar case get <int-id>` |
| Close a case | `secopsctl soar case close --id N --reason "<s>" --yes` |
| Bulk-close | `secopsctl soar push bulk-close --ids a,b,c --reason <enum> --yes` |
| Reconcile SOAR | `soar pull <surface>` → `soar push <surface> --dry-run` → `--yes` |
| Delete server-only | `soar pull <surface>` → `soar push <surface> --prune --dry-run` → `--yes` |
