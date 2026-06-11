# Command reference

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
| `--config <path>` | Use this instance config YAML. An explicit path that does not exist is an error (no silent fall-through). |
| `--json` | Emit machine-readable JSON where supported (shape is per-command). |
| `--legacy` | Force the legacy AppKey path on dual-generation surfaces (currently `soar case list`); ignored where a command has no modern/legacy split. Reach for it when a New-API call 500s. |
| `--non-interactive` | Never prompt; a guarded mutation without `--yes` is refused rather than asking. For CI/agents. |
| `--read-only` | Hard read-only session: every guarded mutation degrades to a dry-run preview even with `--yes`. Also enabled by `SECOPS_READONLY=1` — set it in the environment that launches an autonomous agent. Confirmed mutations and read-only refusals are appended to `~/.secopsctl/audit.jsonl` (`0600`). |
| `-v, --version` | Print version and exit. |
| `-h, --help` | Help for any command. `<cmd> <target> --help` (e.g. `push feeds --help`) adds a per-target note: the surface's plane/version, whether `--prune` can delete it, and its write gotchas. |

**Exit codes** (git-style): `0` success / in sync · `2` divergence — `drift`
detected a difference (act) · `1` any error. A typo'd subcommand also exits
non-zero. Confirm the active config with `secopsctl info` (`config_source` line)
or `secopsctl config --show-path`.

`--json` is honored by the read commands: `info`, `query udm`, `query nl`,
`query raw`, `parsers sample-logs`, `parsers validate`, `entity summarize`, `alerts list`, `alerts get`, `iocs find`, `iocs get`,
`iocs related`, `ti collections`, `ti collection`, `ti related`, `watchlists list`, `watchlists get`,
`curated list`, `curated rules`, `rules detections`, `rules errors`,
`rules retrohunt list`, `rules retrohunt get`, `cases soar-id`, `soar case list`, `soar case get`,
`soar case values`, `soar case comment list`, `soar playbook list`, `soar playbook validate`,
`soar playbook components integrations`, `soar playbook components actions`,
`soar playbook components jobs`, `soar playbook components connectors`,
`soar playbook mold extract`, `soar playbook mold apply`,
`soar playbook trigger set`, `soar playbook test-cases`,
`soar playbook debug-step-data`, `soar playbook simulation-enrichment`,
`soar playbook pending count`, `soar playbook pending list`,
`soar playbook pending get`, `soar playbook step get`,
`soar playbook summary`, `soar playbook results`, `soar playbook result`,
`soar playbook python-logs`, `soar job list`, `soar job template list`,
`soar job instance list`, `soar job logs`, `soar users list`,
`soar marketplace contentpacks get`, `soar integration list`,
`soar integration scaffold`, `info soar-integrations`, `info cron`, `soar build-playbook`,
`soar package-integration`, `soar settings api-keys`, and `version`. It is **also**
emitted by `doctor` (`{ok, version, checks[]}`), `drift` (per-surface report +
`drifted_surfaces`), `push` (the reconcile plan/result + `would_change`), and the
`alerts update`, `soar case`, `soar playbook`, and `soar job` mutating verbs
(dry-run/apply metadata, plus request/response fields where the command has them). Only `pull` is
text-only — its output is the files it writes (review with `git diff`). (`rules
alerts` always emits raw JSON, with or without the flag.)

## 🔒 SIEM — read-only

ADC/OAuth auth (`gcloud auth application-default login`). See
[the loop](the-loop.md), [rules](rules.md), and [query](query.md).

| Command | What it does |
|---|---|
| `info` | Show the resolved instance config (no API call; AppKey redacted). |
| `commands` | List every command with its kind — `read` vs `guarded-mutation` (the `--dry-run`/`--yes` gate) — offline, no credentials. The verb-level companion to `surfaces`; with `--json`, the input for agent tool lists and per-command allowlists. |
| `doctor` | Live smoke test: config + auth + SIEM/SOAR reachability. |
| `pull <target>` | Snapshot live state to local files. Targets: `rules`, `reference_lists`, `data_tables`, `dashboards`, `curated`, `curated_rules`, `feeds`, `parsers`, `rule_exclusions`, `metric_definitions`, `scheduled_reports`, `datataps`, `error_notifications`, `federation_groups`, `all`. `--filter` applies to `curated_rules` only. |
| `drift [target...]` | Report how live state has drifted from local files (CI gate; exit 2 on drift). No target = every engine surface; `--siem`/`--soar` scope to one plane. |
| `query udm <filter>` | Point-in-time UDM event search over `--hours` / `--from` / `--to` (default last 24h), capped by `--limit`. `--raw` prints each matched event's FULL raw ingested log line (for `parsers run --logs -`) instead of the summary — e.g. `query udm 'metadata.log_type = "KONG_GATEWAY" AND metadata.event_type = "GENERIC_EVENT"' --raw --limit 50`. |
| `query nl <text>` | Translate a natural-language query to UDM and search (`--translate-only` to just print the UDM). |
| `query gemini <question>` | Ask SecOps Gemini a question (YARA-L authoring help, UDM fields, environment-grounded answers). `--opt-in` once per account. |
| `query raw <pattern>` | Content-based raw-log search (`searchRawLogs raw = /<pattern>/`) — prints each match's FULL raw ingested log line (for `parsers run --logs -`). Reaches logs with no parser; complements `query udm --raw`. `--unparsed` / `--hours` / `--from`,`--to` / `--limit`. |
| `entity summarize <type> <value>` | Summarize an entity (alerts by rule, related entities, prevalence) over `--hours` (default 7d). |
| `curated list` | List curated (Google-managed) rule-set deployments + enable/alerting state. |
| `curated rules` | List the individual curated rules. |
| `rules list` | List detection rules (rule id · display name · slug · type). The inspect verbs (`detections`/`errors`/`alerts`) accept any of these forms directly. |
| `rules validate <file.yaral>` | Validate a YARA-L file against the API (no mutation); non-zero exit if invalid. |
| `parsers sample-logs <log-type>` | Fetch a sample of a log type's RAW logs directly (`logTypes/<type>/logs`) — full bytes, one per line, to develop a parser against (`--limit` / `--since`). Logs are ordered by resource name (not time), so this is a sample — pass `--since` to bound by time. |
| `parsers validate <log-type>` | Show the parsing errors from the most recently submitted parser's validation report — the detail behind a `push parsers` / `parsers activate` `FAILED_PRECONDITION` (per-log error + a failing-log preview; `--show-logs` for the full sample). |
| `parsers versions <log-type>` | List a log type's parser versions (id · state · created). |
| `parsers run <log-type>` | Validate a CBN parser against sample logs (`--cbn`, `--logs`); no server change. |
| `feeds schemas` | List feed source types (or one source type's log types with `--source-type`) — the field reference for authoring a feed. Templates are in `examples/feed-templates/`; use `secret_ref` for credentials. |
| `rules detections <rule>` | List detections a deployed rule produced in a time window. `<rule>` is a rule id, display name, or slug (resolved against the live rule list). |
| `rules errors <rule>` | List execution errors a rule produced, including structured error payloads. Accepts a rule id, display name, or slug; an unknown rule gives a clean client-side `no rule matches` error instead of an opaque API `400 invalid rule name`. |
| `rules alerts <rule>` | Search alerts a rule generated (raw, rule-dependent shape). Accepts a rule id, display name, or slug. |
| `rules trends` | Per-rule detection counts (day buckets) + last detection over `--hours` (default 7d), noisiest first — which rules are noisy or silent. No `--rule` = every rule. |
| `rules counts` | Rule count and quota statistics for the instance. |
| `rules events <rule> <detection-id>` | The UDM events behind one detection — the evidence pivot (summary per event variable; `--json` for full payloads). |
| `curated detections <ur_id>` | Detections a CURATED rule produced (`ur_…` ids from `curated rules`); the curated twin of `rules detections`. |
| `curated trends (--rule ur_a,… \| --all)` | Per-curated-rule detection counts + last detection; `--all` sweeps every curated rule. |
| `curated events <detection-id>` | Event + rationale behind one curated detection. |
| `alerts list` | List Chronicle detection alerts over a time window (snapshot). |
| `alerts get` | Get one alert by id; when the alert is cased, also prints the SIEM case uuid **and its SOAR integer case id** (the `soar case` pivot). |
| `alerts investigate <id> --latest` | Read the alert's most recent AI (Gemini) investigation: verdict, confidence, summary, suggested next steps (`--json` adds the agent's per-step UDM queries). Without `--latest` it **starts** a new investigation (a generation; refused in read-only mode) and polls to completion. |
| `cases soar-id <uuid>...` | Resolve SIEM case uuid(s) (an alert's `caseName`) to SOAR integer case id(s) — the bridge into every `soar case` verb. |
| `ti collections` | List Mandiant threat collections (campaigns/reports/…). |
| `ti collection <id>` | Show one threat collection by id. |
| `ti related <collection-alt-name-or-id>` | Show IoC match counts for threat collection alt names such as `CAMP.00.001`; resource ids are resolved to alt names first. |
| `iocs find <value>` | Resolve indicator value(s) to IoC records (`--type` to force md5/sha1/sha256/domain/ip; `--from-file <path>`/`-` for a list or stdin). |
| `iocs get <id>` | Get one IoC by its resource id (from `iocs find --json`). |
| `iocs related <ioc-id>` | List campaigns/reports related to an IoC resource id (`--collection-type campaign|report|all`). |
| `watchlists list` | List SIEM entity watchlists. |
| `watchlists get` | Get one watchlist by id. |
| `cases get` / `cases list` / `cases search` | Reach a case on the Chronicle host by UUID — alternate path that 500s today; prefer `soar case`. |
| `version` | Print version, commit, and build info. |

## ⚠️ SIEM — guarded mutations

Dry-run by default; pass `--yes` (or confirm interactively) to deploy. Each
prints a `LIVE DEPLOY` banner. See [rules](rules.md).

| Command | What it does |
|---|---|
| `push rules-create` | Create live rules from `*.yaral` files that have no companion `*.yaml`; `--enabled`, `--alerting`, and `--run-frequency` set the initial deployment. |
| `push rules-update` | Update live YARA-L text where a tracked `*.yaral` changed (etag-guarded). |
| `push rules-deploy` | Reconcile each tracked rule's deployment (enabled/alerting/frequency); `--rule` scopes one rule, and archived rules are reported as non-deployable. |
| `push rules-disable` | Disable locally-tracked rules with `deployment.enabled=true`. |
| `alerts update <id>...` | Set alert triage feedback: `--status new\|reviewed\|closed\|open`, `--verdict true-positive\|false-positive`, `--priority`, `--reason`, `--reputation`, scores, `--comment`, `--root-cause`. Several ids fan out the same update. |
| `alerts run-actions <id> --file <json>` | Execute enrichment-agent actions against a SIEM alert's entities (build the file from `alerts actions`). |
| `watchlists add-entity <id> (--ip\|--mac\|--hostname\|--user\|--email)` | Put one entity on a watchlist — containment/tracking (exactly one selector). |
| `push curated` | Reconcile `curated/deployments.yaml` to live curated deployment state (enabled/alerting only). |
| `push <reconcile-target>` | Reconcile local files to live (create/update; `--prune` deletes on prune-eligible surfaces only — `push <target> --help` says which). Targets: `reference_lists`, `data_tables`, `parsers`, `feeds`, `forwarders`, `dashboards`, `rule_exclusions`, `metric_definitions`, `scheduled_reports`, `datataps`, `error_notifications`, `federation_groups`. |
| `curated set` | Toggle a curated deployment's `enabled`/`alerting` per precision (`--category`, `--ruleset`, `--precision`). |
| `feeds delete <id>` | Delete one feed by id (the feed UUID or full resource name). Stops that feed's ingestion — the explicit one-off, since feeds aren't `--prune`-eligible. Resolves and names the feed before acting. |
| `reference_lists empty <name>` | Clear all entries from one no-delete reference list. Resolves the target and previews entry count only before acting. |
| `rule_exclusions deploy <id>` | Enable, disable, or archive one findings refinement with `--enable`, `--disable`, or `--archive`. Resolves the target and previews current → desired deployment state before acting. |
| `cleanup smoke-artifacts` | Delete or neutralize only secopsctl-owned smoke-test artifacts. Dry-run prints the exact plan; apply requires `--yes`. |
| `rules retrohunt` | Manage retrohunts (run a rule over historical data). |
| `parsers activate <log-type> <id>` | Make a parser version ACTIVE (live ingestion switches; use `parsers versions` to find a prior id to roll back to). |
| `dashboards duplicate <id>` | Copy a dashboard with a new `--name`/`--access` — the supported way to change the immutable `access`. |

## 🔒 SOAR — read-only

AppKey auth (`soar_url` + `$SECOPS_SOAR_APP_KEY`; no ADC). See
[SOAR cases](soar-cases.md) and [reconcile](reconcile.md).

| Command | What it does |
|---|---|
| `soar pull <target>` | Snapshot SOAR state to local files. Targets: `grouping`, `cases`, `blacklists`, `case-stages`, `case-tags`, `close-root-causes`, `connector-allowlist`, `connectors`, `environments`, `idp`, `jobs`, `networks`, `playbook-categories`, `playbooks`, `sla-definitions`, `soc-roles`, `tracking-lists`, `visual-families`, `webhooks`, `all`. |
| `soar playbook list` | List live SOAR playbooks for discovery before editing, saving, or debugging. `--enabled-only` filters to enabled playbooks; `--type regular\|nested` scopes the menu-card query. |
| `soar playbook validate --file <playbook.json>` | Preflight an exported playbook JSON file before save. Runs the same local save-shape checks as `soar push playbook --dry-run` and reports trigger, step, action, block, relation, automatic/manual counts, and graph warnings. |
| `soar playbook components integrations` | List installed integration packs that can supply playbook actions, connector definitions, and job definitions. |
| `soar playbook components actions` | The WHOLE action palette in one call — every action across every integration with its numeric id (the id `components usage` keys on). Add `--integration <key>` for one integration in detail (parameter counts, mandatory parameters, JSON/script-result flags, async state). Neither prints Python script bodies. |
| `soar playbook components flow` | The Flow palette: transformers (value functions, e.g. `trimChars`) and logical operators (condition predicates, e.g. `Not Empty`), with usage examples. `--kind functions\|operators`. |
| `soar playbook components triggers` | The playbook trigger vocabulary (offline): the designer's trigger kinds and the `type` tokens saved on playbook definitions (`ALL`, `CASE_DATA`, `GET_INPUTS`). |
| `soar playbook components blocks` | List playbook BLOCKS — reusable nested playbooks callable as steps. |
| `soar playbook components jobs --integration <key>` | List job definitions inside one integration for workflow planning. |
| `soar playbook components connectors --integration <key>` | List connector definitions inside one integration for workflow planning. |
| `soar playbook test-cases` | List SecOps debug test cases (`--search`, `--environment`, `--page-size`) before a debug run. |
| `soar playbook debug-step-data` | Read simulated case data for one debug step by original step identifier. |
| `soar playbook simulation-enrichment` | Read simulation enrichment for one test case, step, and workflow identifier. |
| `soar playbook pending count` / `list` / `get` | Read pending playbook steps assigned to the current user. `get --case-id N` can be scoped with `--alert-group` and `--workflow-identifier`. |
| `soar playbook step get` | Fetch one workflow step instance by case, workflow, and step identifiers. Use `--json` to save the raw body for guarded `step execute`. |
| `soar playbook summary --case-id N --playbook <name>` | Triage a playbook run: surfaces **FAULTED** steps (action · error · Cloud Logging deep-link; `--show-errors` for the full traceback). `--playbook` resolves to the definition id via `soar playbook list`, and the alert identifier is read from the case — so no opaque GUIDs (override with `--alert`/`--definition`). Prefers the v1alpha path, falls back to legacy. |
| `soar playbook results` / `result` | Read action results for a workflow instance or one case action-result id; human output summarizes status/presence only, `--json` emits the raw payload. |
| `soar playbook versions` | List a playbook's saved version log (each save/deploy mints one); the identifiers feed `restore`. |
| `soar playbook stats` | Aggregate run statistics for one playbook across all cases over `--hours` (default 7d); `summary` stays the single-run view. |
| `soar playbook export` | Export one playbook: definition+blocks JSON (the `mold`/`build-playbook` input), or `--zip --out <f>` for the platform bundle `import` takes. |
| `soar playbook trigger tags` | List the live tag values a Tag-Name trigger condition can reference (`--grep` for a server-side search). |
| `soar playbook components usage (--action-id N \| --action <name>)` | Which playbooks use an integration action (impact analysis). Address it by numeric id (from `components actions`) or by display name, `--integration`-scoped when ambiguous. |
| `soar playbook python-logs` | Read Python execution logs (`--filter`, `--page-size`, `--page-token`). **Note:** this proxies Cloud Logging and can return a server-side 500 on some instances regardless of filter — use `soar playbook summary --case-id N --playbook "<name>"` to triage failed runs instead (it surfaces each faulted step's error + a per-step Logs Explorer link). |
| `soar job list` | List installed SOAR jobs and last-run status without printing job script bodies. |
| `soar job template list` | List SOAR job templates for component planning without printing job script bodies. |
| `soar job instance list` | List configured SOAR job instances. |
| `soar job logs` | Read Python execution logs for SOAR jobs/actions. Use documented filters such as `labels.job_name=~"^."` or `labels.action_name=~"^."`. **Same Cloud Logging caveat as `python-logs`:** can 500 on some instances; for failed playbook/job triage, prefer `soar playbook summary`. |
| `soar case list` | List SOAR cases (default open; `--status open\|closed\|all`, `--limit`). Triage filters: `--assignee` (substring), `--priority`, `--tag` (modern lane), `--since` (duration/timestamp), and a verbatim modern server-side `--filter` expression (grammar below). |
| `soar case counts [--filter <expr>]` | Per-priority case counts for a filter set (default open cases) — one cheap exact count per priority via the list's `totalSize`. |
| `soar case get <id>` | Get one case + its alerts (SOAR integer id). Each alert shows its `--alert` identifier and its **firing rule** (name + `ru_` id) with a `rules detections` pivot hint. |
| `soar case comment list --id N` | List a case's comments (the case-wall record; `--alert` scopes to one alert). |
| `soar case summarize --id N` | The structured AI summary of a case — narrative, reasons, next steps (polls until generation settles). |
| `soar case alert recommend --id N --alert <ident>` | Generate + fetch the AI recommendation for one alert in a case (the alert must be open at alert level; each run starts a generation server-side — refused in read-only mode). |
| `alerts enrich <id>` / `alerts actions <id>` | A SIEM alert's enrichment context / the integration actions executable against its entities (currently blocked server-side — clean error). |
| `info soar-integrations` | Report installed SOAR integration packs, connector/job runtime counts, bound environments, and gaps such as `config_without_runtime` or `runtime_disabled`. |
| `soar integration list` | List installed integration packs. |
| `soar integration instances --integration <id>` | List an integration's configured instances (id · environment · name) — the fields `integration delete` needs, which `list` (packs only) does not expose. |
| `soar integration connector list` | List connector definitions inside an integration (`--integration <key>`; read-only). Sibling `soar integration connector delete` removes a custom definition. |
| `soar marketplace list` | List Content Hub marketplace integrations (`--installed` to filter). |
| `soar marketplace get` | Show one marketplace integration (human summary; `--json` for the full record). |
| `soar marketplace contentpacks` | List Content Hub content packs. |
| `soar settings api-keys` | List SOAR API keys (metadata only; no secret). |
| `soar settings case-assignment` | Read the case auto-assignment policy. |
| `soar settings move-case-policy` | Read the cross-environment case-move policy. |
| `soar case simulation list` | List custom (simulated) test-case names for playbook development. |
| `soar case simulation get --name <sim>` | Read one simulation's alert/event field config. |
| `soar legacy call <op> --read` | Escape hatch: call any Siemplify external-API op read-only (`/api/external/v1`). |

`soar pull grouping` and `soar pull cases` are **snapshot-only** read targets:
there is no matching `soar push grouping`/`push cases` and they are not part of
`drift`, so the pull → diff → push loop does not close for them — use them to
capture state for review, not to reconcile it.

### Case `--filter` grammar (modern cases list)

`--filter` on `soar case list` / `soar case counts` passes a server-side
expression through verbatim — the same grammar the web UI's Case Queue Filter
generates:

| Field | Type | Example |
|---|---|---|
| `status`, `priority` | enum token | `status = 'OPENED'`, `priority = 'PRIORITY_HIGH'` (`PRIORITY_INFO`/`LOW`/`MEDIUM`/`HIGH`/`CRITICAL`) |
| `assignee` | string | `assignee = '@Tier1'` (at-prefixed role name) or a user UUID |
| `environment`, `stage`, `displayName` | string | `stage = 'Triage'` (stages: Triage, Assessment, Investigation, Incident, Improvement, Research) |
| `createTime`, `updateTime` | int64 (epoch ms) | `updateTime >= 1700000000000` |
| `id` | int64 | `id >= 4000` |
| `tags`, `alertNames`, `products` | collection | `any(tags.displayName, 'tag-a', 'tag-b')` · `any(alertNames.alertName, 'RULE NAME')` · `any(products.displayName, 'RULE')` |

Terms compose with `and` / `or` and parentheses. A zero-match query returns an
empty result (HTTP 204), not an error. Very long filters are sent
automatically via the method-override POST the UI uses, so URL length is not a
practical limit.

## ⚠️ SOAR — guarded mutations

Dry-run by default; pass `--yes` to apply. See [SOAR cases](soar-cases.md) and
[reconcile](reconcile.md).

| Command | What it does |
|---|---|
| `soar push <surface>` | Reconcile local files to live (create/update; `--prune` deletes on prune-eligible surfaces only — `soar push <surface> --help` says which). Surfaces: `blacklists`, `case-stages`, `case-tags`, `close-root-causes`, `connector-allowlist`, `connectors`, `environments`, `idp`, `jobs`, `networks`, `playbook-categories`, `playbooks`, `sla-definitions`, `soc-roles`, `tracking-lists`, `visual-families`, `webhooks`. |
| `soar push playbooks` (plural) | Reconcile the **whole** playbooks directory: create/update every changed playbook, `--prune` to delete server-only ones. (One of the reconcile surfaces above.) |
| `soar push playbook` (singular) | Imperative whole-body save of **one** playbook from `--file <playbook.json>`; mints a new version. Dry-run validates JSON and the playbook-name charset offline. Not a directory reconcile — use `playbooks` for the loop. |
| `soar playbook deploy (--name \| --identifier) --enable\|--disable` | Toggle a playbook's `isEnabled`. Reads the full definition, flips the flag, and saves (mints a new version — the only API path). `--name` resolves via the live list. |
| `soar playbook delete (--name \| --identifier)` | Delete a playbook permanently. `--name` resolves via the live list. Irreversible — deleting stops any attached case execution. |
| `soar playbook run` / `debug` | Attach/run a live playbook on an explicit case/alert, or start SecOps debug mode from an exported playbook and explicit test case. Dry-run is default; `--yes` executes in SecOps. |
| `soar playbook rerun` / `rerun-block` | Rerun a playbook or nested block on an explicit case/alert. `rerun-block --inputs <file>` accepts a JSON array of block input parameters. |
| `soar playbook step execute --file <step-instance.json>` | Execute one fetched workflow step instance. Dry-run prints a sanitized summary only; `--yes` sends the exact file body to SecOps. |
| `soar playbook step skip --file <step-instance.json>` | Skip one pending workflow step — the reject half of an approval (`step execute` continues it). `--comment` records why. |
| `soar playbook restore --version <id>` | Roll a playbook back to a version from `versions` (the restore mints a new version; `--override` replaces outright). |
| `soar playbook import --file <bundle.zip>` | Import a playbook bundle (the zip `export --zip` produces) — cross-tenant promotion / backup restore. |
| `soar playbook generate (--description <s> \| --case-id N --alert <id>)` | Draft a playbook with AI (creates a DRAFT on the tenant; generation may run asynchronously — poll the by-alert form with `generate-status`); review via `validate` + the guarded save loop. |
| `soar job instance set --instance <sel> --enable\|--disable` | Enable/disable a scheduled job instance (fresh read, flag flipped, whole body saved). |
| `soar job instance create --file <json>` / `delete --instance <sel>` | Create a scheduled job instance from a JSON body / delete one by id. |
| `soar job run --job <id\|uniqueIdentifier\|name>` | Run one installed SOAR job now. Fetches the live job first, previews the target, and requires `--yes` to execute. |
| `soar job instance run --instance <id\|uniqueIdentifier\|name>` | Run one configured SOAR job instance now. Fetches the live instance first, previews the target, and requires `--yes` to execute. |
| `soar push bulk-close` | Bulk-close cases by id (`--ids`, `--reason` ∈ malicious\|not-malicious\|maintenance\|inconclusive\|unknown). |
| `soar case assign` | Assign a case to a user (`--user`). |
| `soar case tag` / `untag` | Tag / untag a case. |
| `soar case stage` | Change a case's stage (`--stage`). |
| `soar case close` | Close one case (`--id`, `--reason` = the fixed enum `malicious\|not-malicious\|maintenance\|inconclusive\|unknown`, same as `bulk-close`; `--root-cause`, `--comment` optional). |
| `soar case reopen` | Reopen closed case(s) — the inverse of close (`--id` single or `--ids 1,2,3` bulk; `--comment` optional). |
| `soar case priority` | Change a case's priority (`--priority informative\|low\|medium\|high\|critical`; distinct from the `importance` flag). |
| `soar case comment add` | Add a comment to a case (`--id`, `--text`; `--alert` scopes to one alert) — the case-wall triage-rationale record. |
| `soar case alert close` | Close ONE alert in a case — the case stays open (`--id`, `--alert`, `--reason malicious\|not-malicious\|maintenance\|inconclusive`; optional `--root-cause`, `--comment`, `--usefulness none\|useful\|not-useful`). |
| `soar case alert priority` | Change one alert's priority (`--id`, `--alert`, `--priority`); at apply time the alert's name and current priority are resolved from the case, so a wrong `--alert` fails before any mutation. |
| `soar case alert move` | Move one alert out of a case (`--id`, `--alert`; `--to M` for an existing case, omit for a new one) — the inverse of `merge`. |
| `soar case alert reopen` | Reopen one closed alert in a case (`--id`, `--alert`). |
| `soar case rename` / `describe` / `importance` / `merge` | Rename / re-describe / flag-important / merge cases. |
| `soar case run-action --case-id N --action <name> --instance <uuid>` | Execute an integration action on a case (ad-hoc — any installed action). Script params via `--param key=value` (secrets via `env:VAR`). Returns the action result (resultCode, message); `--json` emits the full payload. |
| `soar case simulation create` | Create a custom simulated test case from alert/event field specs — appears in the SOAR queue for playbook testing. |
| `soar case simulation generate --name <sim>` | Generate a test case from a custom simulation name into the case queue. |
| `soar case simulation alert --case-id N --alert <id>` | Simulate an alert inside a case for playbook testing. |
| `soar case simulation delete --name <sim>` | Delete a custom simulation. |
| `soar case values <tags\|stages\|root-causes>` | List the live configured values for `--tag` / `--stage` / `--root-cause`. |
| `soar users list` | List SOAR users (the USERNAME column is the value for `soar case assign --user`); `--grep` / `--all`. |
| `soar integration install --identifier <id>` | Install a Content Hub marketplace integration pack (from `soar marketplace list`); pairs with `uninstall`. |
| `soar integration create` | Create a new, unconfigured (inert) integration instance. |
| `soar integration configure --integration <id> --param k=v` | MUTATING (guarded): set an instance's parameters. Reads current settings, overlays `--param` values (matched on `propertyName` or display name), and saves. `--param 'key=env:VAR'` resolves secrets from env vars. Instance auto-resolved. |
| `soar integration delete --integration <id>` | Delete an integration instance (warns if playbooks use it). `--id`/`--environment` are resolved from the integration's instances — a single instance is auto-selected; several list themselves with copy-paste flags to narrow. |
| `soar integration uninstall` | Delete a custom integration pack (clone) by its key. |
| `soar settings case-assignment` / `move-case-policy` set | Set the case-routing policy (set form is guarded). |
| `soar legacy call <op> --write --yes` | Escape hatch: call any Siemplify external-API mutation. Add `--dry-run` to preview the composed request (method + op + body) without sending; `--out <file>` writes the response `0600`. |

## 🛠️ Utility

| Command | What it does |
|---|---|
| `config` (alias `init`) | Set up / edit the config (`~/.secopsctl/instance.yaml`, `0600`). Single-screen form, or flags + `--non-interactive`. See [configure](configure.md). `config --show-path` prints the active config file. |
| `surfaces [--json]` | List every API surface family — plane (host + auth), API version, lane (reconcile/imperative/raw/operational), status, and whether `--prune` can delete it. Reads nothing live; the map of reconcilable vs read-only. |
| `info cron [--root <dir>] [--host] [--heartbeat-status <label>=<url>]` | Scheduler ownership/orphan report. Scans local scheduler-like files for `secopsctl drift`, `push`, and `soar push` references, plus pulled `soar/jobs/` and `soar/playbooks/` cron schedules. `--host` also inspects the current user's crontab and user systemd unit files. `--heartbeat-status` performs a HEAD check against a read-only status endpoint. Reports file:line references and labels only; raw lines and URLs are not printed. |
| `soar build-playbook --base <playbook.json> --cron <expr> --out <playbook.json>` | Offline SOAR playbook composer. Starts from a full exported base playbook, sets `trigger.cronSchedule`, and can replace placeholder steps with exported, already-wired integration-action step molds via repeated `--replace-step <step>=<step.json>`. |
| `soar playbook mold extract --file <playbook.json> --step <name\|id> --out <step.json>` | Extract one exported action step as a reusable mold for `soar build-playbook`. |
| `soar playbook mold apply --file <playbook.json> --replace-step <step=step.json> --out <playbook.json>` | Replace placeholder steps in an exported playbook with reusable action-step molds, preserving the base step graph identity. |
| `soar playbook trigger set --file <playbook.json> --out <playbook.json>` | Edit trigger fields in exported playbook JSON (`--enabled`, `--trigger-enabled`, `--type`, `--execution-mode`, `--cron`, `--conditions`, `--reaction-conditions`) before validation and guarded save. |
| `soar integration scaffold --name <integration> --out <dir> --action <name> --job <name>` | Offline custom integration scaffold for Python-backed actions/jobs. Package the result with `soar package-integration`; SecOps validates it on import. |
| `soar package-integration <dir>` | Offline ZIP builder for an already-shaped SOAR custom integration directory. Defaults to `<dir>.zip`; use `--out <file>` and `--force` to overwrite. |
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

**Check SOAR integration runtime coverage**:

```bash
secopsctl info soar-integrations
secopsctl --json info soar-integrations
```

**Check scheduled automation references**:

```bash
secopsctl info cron
secopsctl --json info cron --root .
secopsctl info cron --host --heartbeat-status nightly=https://example.com/secopsctl/nightly/status
```

**Develop a SOAR playbook with SecOps-backed tests**:

```bash
secopsctl soar playbook components integrations --grep example
secopsctl soar playbook components actions --integration Example --grep lookup
secopsctl soar playbook mold extract --file exported-playbook.json --step "Lookup" --out molds/lookup.json
secopsctl soar playbook mold apply --file base-playbook.json --replace-step "Lookup=molds/lookup.json" --out playbook.json
secopsctl soar playbook trigger set --file playbook.json --enabled true --type 8 --execution-mode Automatic --cron "0 8 * * *" --out playbook-triggered.json
secopsctl soar playbook validate --file playbook-triggered.json
secopsctl soar playbook test-cases --search smoke --page-size 10
secopsctl soar playbook debug --file playbook.json --test-case-id 123 --dry-run
secopsctl soar playbook debug --file playbook.json --test-case-id 123 --yes
secopsctl soar playbook pending list
secopsctl --json soar playbook step get --case-id 456 --workflow-identifier <workflow-id> --step-identifier <step-id> > step.json
secopsctl soar playbook step execute --file step.json --dry-run
secopsctl soar playbook summary --case-id 456 --playbook "My Playbook"
secopsctl soar playbook results --workflow-instance-id 789
secopsctl soar job logs --filter 'labels.job_name=~"^."' --page-size 20
```

**Scaffold and package a Python-backed SOAR component**:

```bash
secopsctl soar integration scaffold --name ExampleIntegration --action "Lookup Entity" --job "Nightly Sync" --out integrations/ExampleIntegration
secopsctl soar package-integration integrations/ExampleIntegration --out ExampleIntegration.zip
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

**Escape hatch — call a legacy external-API op directly.** When a Siemplify
`/api/external/v1` op has no first-class command, `soar legacy call` reaches it
raw. GET is read-only; the legacy API uses POST for **both** reads and writes, so
a POST must declare intent — `--read` for a read, or `--write --yes` for a
mutation (which prints a live external-API banner; PUT/DELETE are treated as
writes too). Op names and body shapes come
from the SecOps Web UI Network tab (browser dev-tools); the bundled swagger under
`third_party/` is git-ignored and not shipped. Many legacy reads expect an
offset-paging body, `{"requestedPage": 0, "pageSize": 100}`.

```bash
# Read (GET): list installed integrations
secopsctl soar legacy call integrations/GetInstalledIntegrations --read

# Read (POST) with an offset-paging body
printf '{"requestedPage": 0, "pageSize": 100}' > page.json
secopsctl soar legacy call <list-op> --method POST --read --body page.json

# Guarded write (POST): mutation — refused without --yes; --yes deploys live
printf '{"caseId": 1234, "tag": "triaged"}' > req.json
secopsctl soar legacy call <write-op> --method POST --write --body req.json --dry-run  # preview, sends nothing
secopsctl soar legacy call <write-op> --method POST --write --body req.json --yes      # deploy live
```

## 🔗 See also

- [Install](install.md) · [Configure](configure.md) · [The loop](the-loop.md)
- [Rules](rules.md) · [Query](query.md) · [SOAR cases](soar-cases.md) · [Reconcile](reconcile.md) · [SDK](sdk.md)
- [Architecture](../design/architecture.md) · [Surfaces](../design/surfaces.md) · [Catalog](../design/catalog.md) (surface status — source of truth)
- [SIEM design](../design/siem.md) · [SOAR design](../design/soar.md) · [Roadmap](../design/roadmap.md)
- [Glossary](../GLOSSARY.md)
