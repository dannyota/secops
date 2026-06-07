# 🧭 Command reference

Every `secopsctl` command at a glance — what it does, and whether it only
**reads** or performs a **guarded mutation** (live deploy, dry-run by default).
For the step-by-step how-to, follow the per-area guides linked below.

Core loop: pull live state → review in `git diff` → push back. **Every push is a
live production deploy and defaults to a dry run.**

```mermaid
flowchart LR
  live[("live SecOps · SIEM + SOAR")] -- "pull / list / get · read-only" --> files[("local files · git")]
  files -- "git diff → push (dry-run → --yes)" --> live
```

## 🌐 Global flags

Set on any command:

| Flag | Effect |
|---|---|
| `--config <path>` | Use this instance config YAML (overrides `$SECOPSCTL_CONFIG` and discovery). |
| `--json` | Emit machine-readable JSON where supported (shape is per-command). |
| `--legacy` | Force the legacy AppKey path only, skipping the modern v1alpha API — for surfaces that support both. Reach for it when a New-API call 500s. |
| `-v, --version` | Print version and exit. |
| `-h, --help` | Help for any command. |

## 🔒 SIEM — read-only

ADC/OAuth auth (`gcloud auth application-default login`). See
[the loop](the-loop.md), [rules](rules.md), and [query](query.md).

| Command | What it does |
|---|---|
| `info` | Show the resolved instance config (no API call; AppKey redacted). |
| `doctor` | Live smoke test: config + auth + SIEM/SOAR reachability. |
| `pull <target>` | Snapshot live state to local files. Targets: `rules`, `reference_lists`, `data_tables`, `dashboards`, `curated`, `curated_rules`, `feeds`, `parsers`, `rule_exclusions`, `metric_definitions`, `scheduled_reports`, `datataps`, `error_notifications`, `federation_groups`, `all`. `--filter` applies to `curated_rules` only. |
| `drift [target...]` | Report how live state has drifted from local files (CI gate; non-zero exit on drift). No target = every engine surface. |
| `query udm <filter>` | Point-in-time UDM event search over `--hours` / `--from` / `--to` (default last 24h), capped by `--limit`. |
| `curated list` | List curated (Google-managed) rule-set deployments + enable/alerting state. |
| `curated rules` | List the individual curated rules. |
| `rules detections` | List detections a deployed rule produced in a time window. |
| `rules errors` | List execution errors a rule produced. |
| `rules alerts` | Search alerts a rule generated (raw, rule-dependent shape). |
| `alerts list` | List Chronicle detection alerts over a time window (snapshot). |
| `alerts get` | Get one alert by id. |
| `ti collections` | List Mandiant threat collections (campaigns/reports/…). |
| `ti collection <id>` | Show one threat collection by id. |
| `iocs find <value>` | Resolve indicator value(s) to IoC records (`--type` to force md5/sha1/sha256/domain/ip). |
| `iocs get <id>` | Get one IoC by its resource id (from `iocs find --json`). |
| `watchlists list` | List SIEM entity watchlists. |
| `watchlists get` | Get one watchlist by id. |
| `cases get` / `cases list` / `cases search` | Reach a case on the Chronicle host by UUID — alternate path that 500s today; prefer `soar case`. |
| `version` | Print version, commit, and build info. |

## ⚠️ SIEM — guarded mutations

Dry-run by default; pass `--yes` (or confirm interactively) to deploy. Each
prints a `LIVE DEPLOY` banner. See [rules](rules.md).

| Command | What it does |
|---|---|
| `push rules-create` | Create live rules from `*.yaral` files that have no companion `*.yaml`. |
| `push rules-update` | Update live YARA-L text where a tracked `*.yaral` changed (etag-guarded). |
| `push rules-deploy` | Reconcile each tracked rule's deployment (enabled/alerting/frequency). |
| `push rules-disable` | Disable locally-tracked rules with `deployment.enabled=true`. |
| `push <reconcile-target>` | Reconcile local files to live (create/update; `--prune` to delete). Targets: `reference_lists`, `data_tables`, `parsers`, `feeds`, `forwarders`, `dashboards`, `rule_exclusions`, `metric_definitions`, `scheduled_reports`, `datataps`, `error_notifications`, `federation_groups`. |
| `curated set` | Toggle a curated deployment's `enabled`/`alerting` per precision (`--category`, `--ruleset`, `--precision`). |
| `rules retrohunt` | Manage retrohunts (run a rule over historical data). |

## 🔒 SOAR — read-only

AppKey auth (`soar_url` + `$SECOPS_SOAR_APP_KEY`; no ADC). See
[SOAR cases](soar-cases.md) and [reconcile](reconcile.md).

| Command | What it does |
|---|---|
| `soar pull <target>` | Snapshot SOAR state to local files. Targets: `grouping`, `cases`, `blacklists`, `case-stages`, `case-tags`, `close-root-causes`, `connectors`, `environments`, `idp`, `jobs`, `networks`, `playbook-categories`, `playbooks`, `sla-definitions`, `soc-roles`, `tracking-lists`, `visual-families`, `webhooks`, `all`. |
| `soar case list` | List SOAR cases (default open; `--status open\|closed\|all`, `--limit`). |
| `soar case get <id>` | Get one case + its alerts (SOAR integer id). |
| `soar integration list` | List installed integration packs. |
| `soar integration connector list` | List connector definitions inside an integration (`--integration <key>`; read-only). Sibling `soar integration connector delete` removes a custom definition. |
| `soar marketplace list` | List Content Hub marketplace integrations (`--installed` to filter). |
| `soar marketplace get` | Show one marketplace integration. |
| `soar marketplace contentpacks` | List Content Hub content packs. |
| `soar settings api-keys` | List SOAR API keys (metadata only; no secret). |
| `soar settings case-assignment` | Read the case auto-assignment policy. |
| `soar settings move-case-policy` | Read the cross-environment case-move policy. |
| `soar legacy call <op> --read` | Escape hatch: call any Siemplify external-API op read-only (`/api/external/v1`). |

## ⚠️ SOAR — guarded mutations

Dry-run by default; pass `--yes` to apply. See [SOAR cases](soar-cases.md) and
[reconcile](reconcile.md).

| Command | What it does |
|---|---|
| `soar push <surface>` | Reconcile local files to live (create/update; `--prune` to delete). Surfaces: `blacklists`, `case-stages`, `case-tags`, `close-root-causes`, `connectors`, `environments`, `idp`, `jobs`, `networks`, `playbook-categories`, `playbooks`, `sla-definitions`, `soc-roles`, `tracking-lists`, `visual-families`, `webhooks`. |
| `soar push playbook` | Save a playbook definition (whole-body replace; mints a new version). |
| `soar push bulk-close` | Bulk-close cases by id (`--ids`, `--reason` ∈ malicious\|not-malicious\|maintenance\|inconclusive\|unknown). |
| `soar case assign` | Assign a case to a user (`--user`). |
| `soar case tag` / `untag` | Tag / untag a case. |
| `soar case stage` | Change a case's stage (`--stage`). |
| `soar case close` | Close one case (`--id`, `--reason` string; `--root-cause`, `--comment` optional). |
| `soar case rename` / `describe` / `importance` / `merge` | Rename / re-describe / flag-important / merge cases. |
| `soar integration create` | Create a new, unconfigured (inert) integration instance. |
| `soar integration delete` | Delete an integration instance (warns if playbooks use it). |
| `soar integration uninstall` | Delete a custom integration pack (clone) by its key. |
| `soar settings case-assignment` / `move-case-policy` set | Set the case-routing policy (set form is guarded). |
| `soar legacy call <op> --write --yes` | Escape hatch: call any Siemplify external-API mutation. |

## 🛠️ Utility

| Command | What it does |
|---|---|
| `config` (alias `init`) | Set up / edit the config (`~/.secopsctl/instance.yaml`, `0600`). Single-screen form, or flags + `--non-interactive`. See [configure](configure.md). |
| `completion` | Generate the shell autocompletion script. |
| `help` | Help about any command. |

## 🧪 Cookbook

End-to-end recipes. The deeper how-to lives in the per-area guides.

**Prove the setup before touching anything** ([install](install.md),
[configure](configure.md)):

```bash
secopsctl config     # write ~/.secopsctl/instance.yaml (git-ignored, 0600)
secopsctl doctor     # read-only reachability check: SIEM + SOAR
secopsctl info       # show resolved config (AppKey redacted; no API call)
```

**The golden rule** — reads are free; every write is dry-run first
([the loop](the-loop.md)):

```bash
secopsctl push <target>          # dry-run by default — read the preview
secopsctl push <target> --yes    # apply for real
```

**Edit a detection rule** ([rules](rules.md)):

```bash
secopsctl pull rules                       # always pull before you edit
# edit ./rules/<slug>.yaral, then: git diff rules/
secopsctl push rules-update --dry-run      # etag-guarded preview
secopsctl push rules-update --yes
secopsctl pull rules                        # re-pull so local matches live
```

**Triage a case** — `--id` is the SOAR integer id from `soar case list`
([SOAR cases](soar-cases.md)):

```bash
secopsctl soar case list
secopsctl soar case get 1234
secopsctl soar case close --id 1234 --reason "Malicious" --yes
```

**Reconcile a SOAR surface** ([reconcile](reconcile.md)):

```bash
secopsctl soar pull webhooks               # snapshot the whole surface
# edit ./soar/webhooks/, then: git diff soar/webhooks/
secopsctl soar push webhooks --dry-run     # additive preview
secopsctl soar push webhooks --yes
secopsctl soar push webhooks --prune --yes # delete server-only objects (gated on a full pull)
```

**Ad-hoc UDM search** ([query](query.md)):

```bash
secopsctl query udm 'metadata.event_type = "USER_LOGIN"' --hours 48 --limit 500 --json
```

## 🔗 See also

- [Install](install.md) · [Configure](configure.md) · [The loop](the-loop.md)
- [Rules](rules.md) · [Query](query.md) · [SOAR cases](soar-cases.md) · [Reconcile](reconcile.md) · [SDK](sdk.md)
- [Architecture](../design/architecture.md) · [Surfaces](../design/surfaces.md) · [Catalog](../design/catalog.md) (surface status — source of truth)
- [SIEM design](../design/siem.md) · [SOAR design](../design/soar.md) · [Roadmap](../design/roadmap.md)
- [Glossary](../GLOSSARY.md)
