# Catalog — SOAR & cross-cutting surface details

Per-surface detail for the SOAR (Siemplify) plane and the cross-cutting features
(Threat Intel, Content Hub). The status matrix and the compact tables are the spine
in [catalog.md](catalog.md); this page expands each row into a `##### <function>`
entry. Keep the two in lock-step.

## Control plane — config as code (`soar pull` → `git diff` → `soar push`)

### `webhooks`

Full CUD; create is license-capped (engine surfaces the error, smoke skips). **PruneEligible.**

### `environments`

Modern write endpoint validated (does not 500) via `TestLiveConfigSurfaceWriteSmoke`; create reachable but license-capped. Legacy reconcile NoDelete (segregation unit — high blast), writes guarded. Modern API: siemplify · v1alpha (create/get/update/delete wired).

### `networks`

Modern API: siemplify · v1alpha (`soarNetworks` — CRUD wired in `soar/data_surfaces.go`; create→get→delete **write-validated** `TestLiveDataSurfaceWriteSmoke`; `deleteAll`/`export`/`import` documented). Write smoke uses RFC5737 throwaway. **PruneEligible**: `DeleteNetwork(id)` is a clean by-id delete (`TestLiveReconcileNetworkDeleteByIDSmoke`); low-blast enrichment data.

### `tracking-lists`

First write-loop proof (clone throwaway).

### `soc-roles`

RBAC. Modern API: siemplify · v1alpha (CRUD wired; create→get→delete live-validated, `TestLiveConfigSurfaceWriteSmoke`). Modern write does not 500. Legacy write **path** validated via an inert throwaway role — create→rename→delete (no users assigned); `DeleteSocRole` takes `{socRoleId}`. **Engine-NoDelete** (delete via raw SDK only; `--prune` never deletes these) — reconcile RBAC with care.

### `idp`

SSO; id-from-body update closure. Write **path** validated via a throwaway mapping for a **fake group** (no real users) — create→rename→delete-by-id. **Engine-NoDelete** — reconcile SSO with care.

### `visual-families`

Write smoke; validates the `wrapKey` envelope. **PruneEligible**: `DeleteFamilyData` is a clean by-id delete on an inert custom family.

### `sla-definitions`

Affects alert routing. Modern API: siemplify · v1alpha (`slaDefinitions` — CRUD wired; create→get→delete **write-validated** `TestLiveDataSurfaceWriteSmoke`; `export`/`import` documented). Uses **string** enums `SlaType`/`SlaTimeUnit`, not the legacy ints. Write **path** validated via a throwaway "Case Priority = High" SLA. Legacy `ApiSlaDefinition` int enums are documented in the **swagger schema `description`** fields: `valueType` (`ApiSlaProviderTypeEnum`) 2=AlertRuleGenerator / 3=CaseStage / **4=CasePriority** / 5=AlertPriority; `slaPeriodType`/`criticalPeriodType` (`ApiPeriodTypeEnum`) 0=Minutes / **1=Hours** / 2=Days / 3=Seconds; `alertType` (`ApiSlaAlertType`) **0=AllAlerts** / 1=SpecificAlerts. For CasePriority the `value` round-trips as a JSON-array string (`["High"]`). **Engine-NoDelete** (delete via raw `RemoveSlaDefinitionRecords`) — routing surface, reconcile with care.

### `case-stages`

Wrapped list. Modern API: siemplify · v1alpha (caseStageDefinitions create/delete wired; the v1alpha case-defs do not 500). Legacy write **path** validated via an inert throwaway stage — create→reorder→delete (used by no case); `RemoveCaseStageDefinitionRecords` takes the full record. **Engine-NoDelete** — UI-pollution, reconcile with care.

### `case-tags`

Modern API: siemplify · v1alpha (caseTagDefinitions create/get/delete **live-validated**, `TestLiveConfigSurfaceWriteSmoke`). Modern create→get→delete works (does not 500; `priority` must be > 0). Legacy write smoke skips (no tag to clone).

### `close-root-causes`

Modern API: siemplify · v1alpha (caseCloseDefinitions create/delete wired; case-defs family validated, no 500). Legacy: non-unique names exercises the slug-collision fix.

### `blacklists`

Modern API: siemplify · v1alpha (`entitiesBlocklists` — read + create/get/delete wired in `soar/data_surfaces.go`). Writes reach the endpoint (HTTP 400, **not** 500) but `action` (`ActionScope`) and `entityType` are undocumented server enums — supply a valid token; **not write-validated**. Model block-list; legacy write smoke.

### `playbook-categories`

Write smoke.

### `playbooks`

uuid rotates → key on **name**; whole-body save; `SavePlaybook` update-by-name verified. `soar playbook export` (default JSON) emits the camelCase, string-enum `GetPlaybook` shape — the same one `pull playbooks` writes and the save accepts — so export → edit → `push playbook` round-trips and the file is what `mold`/`build-playbook` consume. The old `ExportWorkflowWithBlocks` PascalCase/int-enum bundle was save-incompatible and consumed by nothing; the platform bundle is `--zip`. Inline secrets are redacted on pull (see pull-time redaction below); the save refuses a body still carrying the redaction marker. Local playbook loads and singular `soar push playbook --dry-run` validate the save-time name charset (`[A-Za-z0-9 _-]`) before any API call. Interaction helpers: `soar playbook list` discovers live playbooks from SecOps; `soar playbook validate --file` runs the save preflight and reports trigger/step/action shape before a guarded save. Component helpers list installed integrations, actions, jobs, and connector definitions; `soar playbook mold extract/apply` lifts and applies reusable action-step molds from exported playbook JSON; `soar playbook trigger set` edits reviewable trigger fields before validation/save. Operational helpers cover SecOps debug/run/readback (`test-cases`, `run`, `debug`, `summary`, `results`, `result`, `python-logs`, `debug-step-data`, `simulation-enrichment`, `rerun`, `rerun-block`). Playbooks/workflows exist **only** on Legacy — no New twin.

### `connectors`

Moved onto the reliable Legacy engine (replaces a former modern v1alpha pull+patch). **Full CUD** — create + whole-body update (`SaveConnector`) + delete-by-id (`DeleteConnector`, **PruneEligible**). `SaveConnector` upserts both: **create** triggers when the body has **no `identifier`** (server assigns one); a client-assigned id routes to update (404 if absent) — a new connector file omits `identifier`, so engine create works. `ListConnectorCards` groups cards by integration, so the list closure **flattens** them. Secret params arrive server-masked and pass through unchanged on update. `extraStrip` = `version`/`isUpdateAvailable`/`loggingEnabledUntilUnixMs`/`isCustom`. `TestLiveReconcileConnectorWriteSmoke` (throwaway DISABLED connector → create → update → delete; self-cleaning).

### `connector-allowlist`

Alert allow-list view over connector instances. `pull` writes sanitized files under `soar/connector_allowlist/` with context plus `allowList`; drift compares only `allowList`. `push` is update-only: it fresh-reads the full connector body, replaces `allowList`, and calls `SaveConnector`, preserving parameters/secrets and refusing non-empty allow-lists on connectors that do not support them. Live write-smoke `TestLiveConnectorAllowlistWriteSmoke` performs an idempotent same-value save and verifies the pulled before/after snapshots stay identical.

### `jobs`

Legacy engine (replaces a former modern v1alpha pull+patch). pull + update (`SaveOrUpdateJob` whole-body upsert; the installed-job read item IS the write body); **NoDelete** (delete takes a body, not a clean id). `extraStrip` drops `version`/`lastRunStatus`/`lastRunTime`/`creator`. `TestLiveReconcileJobWriteSmoke` (throwaway DISABLED job → update → raw delete; self-cleaning).

### `grouping`

Alert-grouping rules as config-as-code on the v1alpha SOAR host, via a bespoke `reconcile.Surface` backed by the MODERN soar client (does not ride the legacy jsonSurface adapter). `pull grouping` writes `grouping/rules/<slug>.json` (writable config — category/groupingType/entityType/categoryDetails + a `_server` id) and the General/Overflow settings singleton to `grouping/settings.json` (read from the legacy `GetMaximumAlertsGroupingConfiguration` config endpoint, modern `moduleSettings` fallback). The settings singleton is readable via the imperative **`soar settings grouping get`** (W80) — which surfaces the legacy max-alerts-per-case value plus any modern moduleSettings properties. `push grouping [--prune]` reconciles create/update/delete, dry-run by default; **`--prune` refuses the non-deletable catch-all fallback (`category: ALL`)**. SDK rule lifecycle write-validated — create→get→delete on a self-cleaning inert throwaway (`TestLiveAlertGroupingRuleWriteSmoke`); the reconcile surface is offline-tested (round-trip, fallback guard) with the live reconcile write gated. Numeric `id` decoded as number-or-string.

### `case-data` — `customFields` · `calculatedFieldDefinitions` · `propertySchemaDefinitions`

Wave 16. Modern API: siemplify · v1alpha — SDK full CRUD (`soar/case_data_surfaces.go`); create→get→delete **write-validated** (`TestLiveCaseDataWriteSmoke`). No CLI wired yet. `customFields` `scopes`="Case"/"Alert" (FREE_TEXT needs no options; **"All" 500s**). `calculatedFieldDefinitions` is logic-as-code (`SET_VALUE`/`TEXT`/`targetField=CaseCustom.<field>`/`formulaExpression="…"`) and depends on a Free-Text custom field — create field→calc, delete calc→field.

## Operational + imperative — query → act / per-entity verbs

### `cases list` / `get <id>` (alias `soar case …`)

A case is one record, surfaced as one command: the top-level `cases` command is the
canonical spelling and `soar case …` remains as a hidden back-compat alias (the same
verb tree). `cases list` defaults to the modern v1alpha cases API on the siemplify domain (`soar.ListCases`, live-validated) and auto-falls back to the legacy queue (`ListCaseCards`) on error; `--legacy` forces legacy. `list` sends the web-UI query params — server-side `filter` (`--status` → `status = 'OPENED'`/`'CLOSED'`), `orderBy=updateTime desc`, and `expand` (products/tasks/tags/closureDetails/sla/alertsSla) via `ListCasesOpts` (client-side status re-check kept as a safety net); live-validated (open vs closed return different counts).

**Triage filters (W52, live-validated):** `--assignee` (substring) / `--priority` / `--tag` / `--since` narrow the fetched page client-side on both lanes (`--tag`/`--filter` are modern-lane and fail loud on the legacy queue); `--filter` passes a verbatim server-side expression to the modern API (`priority = 'PRIORITY_HIGH'` confirmed honored server-side).

`get <id>` uses legacy `GetCaseFullDetails` → case and its alerts (each with its `--alert` id for the verbs) — and per alert the firing rule (`additionalProperties.ruleGenerator` + `rule_id`) with a `rules detections` pivot hint (W52, live-validated): the case→rule-tuning bridge. Shared `preferModern` helper.

**Chronicle-host path (not surfaced):** the same case is reachable on the chronicle host by UUID (ADC), but that collection errors at v1/v1beta/v1alpha — so W85 removed the dead `cases list/get/search` verbs and the `cases (chronicle alt)` surface entry; case work runs entirely on the working siemplify path above. The one chronicle-host case read kept is the bridge: `legacyBatchGetCases` maps SOAR int id ↔ SIEM UUID — CLI `cases soar-id`, and `alerts get` prints it per alert. (`chronicle.ListCasesOpts`/`GetCase`/`SearchCases` remain importable for the day the collection stabilizes.)

**Bulk close from a filter (W77):** `soar push bulk-close --where <filter>` selects cases by a modern cases-list filter (alternative to `--ids`), resolves them to integer ids (listed + counted before the guard), and closes with a typed reason.

### `soar case summarize` / `counts` / `alert recommend` (AI)

`summarize --id N` polls `cases:getOrCreateCaseSummary` to a settled state and prints the structured Gemini case summary (summary · reasons · next steps) — live-validated (a full real summary round-tripped).

`counts [--filter]` (W59, live-validated) derives per-priority counts from the modern list's `totalSize` (one `pageSize=1` count per priority, composed with the base filter; `soar.CountCases`/`CountCasesByPriority`) — the `cases:countPriorities` RPC is not served (404/500; the web UI builds its queue from filtered lists) and the SDK method remains only for instances that may serve it.

`alert recommend --id N --alert <ident|numeric>` triggers + polls the per-alert AI recommendation; the CREATE leg works against an alert-level-open alert (`caseAlerts:createRecommendationLongRunning` → 202 + recommendationId, numeric id accepted) but `:fetchRecommendation` is not served (400 on every documented form) — where the recommendation feature is absent, the per-alert AI flow is the chronicle-host investigation instead (`alerts investigate`); the verb stays wired and surfaces a clean error.

### `soar case overview`

`overview --id N` returns the data behind the console's case Overview tab (legacy `case-overview/GetCaseEntities`): the case's entities with their enrichment fields — the entity context an analyst sees — live-validated (40 entities returned for a real case). `--widgets` returns the overview widget template instead (`GetCaseOverviewData`, body `{caseId}`). Read-only, JSON; completes the case read trio with `summarize` (AI narrative) and `get` (record + alerts).

### `soar case run-action`

`run-action --id N --action <name> --instance <uuid>` wraps `legacyCases:executeManualAction` — runs any installed integration action on a case with `--param key=value` script parameters (secrets via `env:VAR`). Returns the full action result (resultCode, message, resultJsonObject); `--json` emits the raw payload. Guarded; live dry-run validated.

### `soar case simulation`

Playbook-dev test harness: `list` / `get` / `create` / `generate` / `alert` / `delete` wrapping the `legacyCases` simulation surface (`getCustomCases`, `getCustomCaseDetails`, `createSimulatedCustomCase`, `generateUseCases`, `simulateAlert`, `deleteUseCase`). Full write round-trip live-validated (create→list→generate→delete with a throwaway simulation). All guarded.

### `soar case <verb>` (assign/rename/stage/tag/untag/describe/importance/priority/close/reopen/merge + `comment add`)

The reliable operational case path. The original 9 mutate verbs are swagger-verified + unit-tested and live-validated end-to-end by `TestLiveSOARCaseVerbsWriteSmoke` (create two throwaway cases → run every verb → merge → close).

**W52 adds** `priority` (`ChangeCasePriority`, typed `CasePriority` names — distinct from the `importance` flag), `reopen` (`ExecuteBulkReopenCase`; single `--id` or bulk `--ids`, the inverse of close), and `comment add`/`comment list` (`AddCaseComment` + `GET /cases/comments` — the case-wall triage-rationale record, distinct from `chat`); the write smoke is extended to cover them (comment round-trip read-verified) and awaits an approved run — dry-run + comment-list read validated live.

**Bulk triage (W95):** `assign`, `tag`, and `stage` take `--ids 1,2,3` for a single bulk call alongside the single `--id` form — `ExecuteBulkAssign` (`{casesIds, userName}`), `ExecuteBulkAddCaseTag` (`{casesIds, tags}`), `ExecuteBulkChangeCaseStage` (`{casesIds, stage}`); request shapes confirmed against the live API (a bulk-tag round-trip applied + read-verified + reverted). `untag` stays single (no bulk-remove-tag endpoint).

Built on a typed `CreateManualCase` (`ManualCaseRequest`, returns the new case id) that always sends `entities`/`playbooks`/`tags` as `[]` (the server does not null-guard them). `merge` needs the target id in `casesIds` (CLI adds it). `assignedUser` takes `@RoleName`. Hard delete (`RetentionDeleteCases`) is denied to the AppKey role (403) → smoke cleans up by closing.

### `soar case alert <verb>` (close/priority/move/reopen)

Per-ALERT triage inside a case (W52):

- `close` one false-positive alert without closing the case (`CloseAlert`; reason vocabulary excludes `unknown`, optional `--usefulness` stat)
- re-prioritize one alert (`UpdateAlertPriority` — the CLI resolves the alert's name + current priority from the case at apply time, so a wrong `--alert` fails on that read before any mutation; dry runs stay credential-free)
- split a mis-grouped alert out (`MoveAlertToNewCase` — `--to` an existing case, or omit for a new one; the inverse of `merge`)
- reopen a closed alert (`ReopenAlert`)

All on the standard guard; dry-run + error paths validated live; writes ride the extended `TestLiveSOARCaseVerbsWriteSmoke`, gated.

### `soar playbook generate` (AI drafting)

W56. Gemini playbook drafting: `generate --description <text>` or `generate --case-id N --alert <id>` ("build a playbook for this alert pattern") over `legacyPlaybooks:legacyAiGenerate`/`:legacyAiGenerateByAlert` (+ SDK `AiUpdatePlaybook` / `AiGenerationStatusByAlert`). Generation creates a draft playbook on the instance → guarded; the result flows through the standard review loop (`validate` → `push playbook --dry-run` → save). Live write gated.

### `soar playbook` operational helpers

Waves 39 + 51 + 55. Read helpers: list/validate/components integrations/actions/jobs/connectors/test-cases/debug-step-data/simulation-enrichment/pending/step get/summary/results/result/python-logs.

**W55 lifecycle ops (read-validated live):**

- `versions` (the per-save version log — empty until a save mints one) + guarded `restore --version` (rollback; the restore itself mints a new version)
- `stats` (cross-case run statistics, modern bridge `legacyGetPlaybookStatsMap` with legacy fallback)
- `export` (definition+blocks JSON — the mold/build-playbook input — or `--zip` the platform bundle, ApiFile blob decoded) + guarded `import --file` (cross-tenant promotion)
- `trigger tags` (live Tag-Name trigger vocabulary)
- `components usage` (which playbooks use an action — W58: by `--action-id` or by `--action <name>`, resolved through the wildcard action catalog)
- guarded `step skip` (the reject half of an approval; `step execute` is the continue half — the SDK's historical `SkipAlert` name is deprecated in favor of `SkipStep`, which overlays `skipComment` onto the exact fetched bytes so int64 step ids survive; the preview surfaces `is_skippable` + the comment, with a warning when the fetched body says not skippable)

`python-logs` proxies Cloud Logging and can 500 on some instances — `summary` is the recommended triage path (surfaces faulted steps + per-step Logs Explorer link). Guarded mutation: `deploy (--name|--identifier) --enable|--disable` toggles `isEnabled` via a read-flip-save on the full definition (`SaveWorkflowDefinitions`, mints a new version — the only API path); `delete` supports single (`--name|--identifier`) and batch (`--identifier a,b,c` or `--from-file <path>`, one UUID per line) — batch uses a single `legacyDeleteWorkflows` API call and reports per-playbook success/failure; single uses `preferModern` with legacy fallback. All playbook types (certified, Content Hub, custom, nested blocks) are deletable — no type is protected.

Guarded execution: `run`, `debug`, `rerun`, `rerun-block`, and `step execute` all dry-run by default and require explicit case/test-case/workflow/step selectors before SecOps executes anything. Human output summarizes counts/status/presence only; `--json` returns raw SecOps payloads for scripts. Live write not run by default because playbooks can create cases, tasks, alerts, and external side effects.

`summary` surfaces a run's faulted steps (action · error/traceback · per-step Logs Explorer link) and resolves `--playbook` name → definition id + the case's alert id (`alerts[].additionalProperties.alertGroupIdentifier`), so no opaque GUIDs are needed; it prefers the v1alpha `legacyPlaybooks:legacyGetWorkflowInstanceSummary` path with legacy fallback (live-validated).

**W64:** offline `step insert` splices a brand-new action step into the graph (fresh identity, rewired relations; `legacy.InsertActionStep`), and every playbook save path decodes int64-safely (`UseNumber`).

### playbook authoring palette (`soar playbook components`)

The designer's Step Selection palette as CLI catalogs (W58, all live-validated):

- `components actions` with no flag lists every action across every integration in one call (`integrations/-/actions`, field-masked summary + the numeric definition id; `--integration` keeps the one-pack detail view)
- `components flow` lists transformers (value functions) + logical operators (condition predicates) with usage examples (`integrations/-/{transformers,logicalOperators}`; the logical-operators envelope key is snake_case even under `format=camel`)
- `components triggers` prints the trigger vocabulary offline (designer kinds → the saved `type` tokens `ALL`/`CASE_DATA`/`GET_INPUTS`; triggers have no list API)
- `components blocks` lists the reusable nested playbooks

SDK: `ListAllActions`/`ListActions`/`ListTransformers`/`ListLogicalOperators` (`soar/integrations.go`), typed `ActionDef`/`FlowFunction` with numeric-id addressing.

### `soar job` operational helpers

Waves 39 + 55. `soar job list`, `soar job template list`, and `soar job instance list` fetch legacy job/runtime catalogs without printing script bodies.

**W55 instance management (guarded; dry-run validated):**

- `instance set --enable|--disable` (fresh read + byte-preserving `isEnabled` overlay + whole-body PUT — the disable-a-noisy-scheduled-job path; the swagger's update shape declares `jobDefinitionId`/`jobDefinitionName`, which live list records do not carry, so the gated idempotent same-value smoke `TestLiveJobInstanceSetWriteSmoke` verifies the PUT shape before any real toggle)
- `instance create --file` (exact file bytes on the wire)
- `instance delete` (clean by-id DELETE, unlike the body-taking definition-level delete)

Set/delete resolve the target with a live list read before the guard (the dry-run line says so). `soar job logs` reads Python execution logs for SOAR jobs/actions with Cloud Logging filters such as `labels.job_name=~"^."`; human output prints counts only and `--json` emits the raw payload. `soar job run` and `soar job instance run` resolve one explicit id/uniqueIdentifier/name, preview the target, and require `--yes` before SecOps executes it. `--json` returns dry-run/apply metadata and raw response when applied. Live execution is not run by default because jobs can create cases, tasks, alerts, and external side effects.

### `soar push bulk-close`

Queue bulk-close (`ExecuteBulkCloseCase`). Takes the fixed reason enum (malicious/not-malicious/maintenance/inconclusive/unknown) — the same vocabulary single-case `close --reason` now takes (W34), so the two aggregate in metrics. Bulk sends the integer `closeReason`; single sends the PascalCase enum name (e.g. `NotMalicious`) the legacy `Api*` close family expects on the wire (`soar users list` is the assignee directory for `assign`).

### `soar settings case-assignment` / `move-case-policy` (`get`/`set`)

Singleton case-routing policies (one record, no id/list/delete) → imperative get/set, not reconcile. `get` read-only; `set <enum>` guarded.

### `soar settings grouping` (`get`/`set`)

W80. The alert-grouping General/Overflow settings singleton the `push grouping` RULES reconcile can't reach.

`get` (live-validated) reads the reliable legacy max-alerts-per-case value (`GetMaximumAlertsGroupingConfiguration`) AND any modern `moduleSettings/AlertGroupingSettings/properties` (the modern bag is empty on many instances, so the General/Overflow knobs are then read-mostly, edited in SOAR Settings). Guarded `set --property name=value` applies via `BatchUpdateModuleSettingProperties` where the instance exposes writable properties; the max-alerts singleton has no API SET (swagger has only the GET).

### `soar settings api-keys` (list/create/revoke)

SOAR external-API key administration on `/settings/` (all three ops absent from the swagger; W60, lifecycle live-validated — create → list → revoke → verified gone).

`list` (`GET /settings/GetApiKeys`) is metadata only: the server masks the key and the typed view drops it (House Rule 4). Guarded `create --name --permission-group` (`POST /settings/addOrUpdateApiKeyRecord`, no id = create): the key value is client-generated — a crypto/rand UUID minted locally, stored verbatim server-side, shown exactly once on success, never logged or persisted (the audit log records action+decision only); a null `socRoleId` is server-defaulted. Guarded `revoke (--name|--id)` (`POST /settings/RemoveApiKeyRecord`) posts the full record as listed (the typed `APIKey` retains its verbatim Raw form for exactly this). `soar/legacy/settings_api_keys.go`.

### `idp-mappings`

SDK-only (no CLI yet). IdP group → SOC-role / permission-group / environment mappings (`legacySoarIdpMappingGroups`). SDK `List`/`Get`/`Create`/`Update`/`Delete` + `GetExternalProviders` (`soar/idp_mappings.go`).

Two-host surface: the v1alpha docs file it under the chronicle instance path, but it 500s on chronicle and answers on the SOAR host (AppKey) — so it lives on the SOAR plane (the registry codes it `AreaSOAR` / SOAR-modern). Read-validated live (mapping groups + external providers). Writes touch live access — gate carefully.

### `form-dynamic-parameters`

Investigated as a reconcile surface but not wired — the strict PUT update silently resets a parameter's `formType` to Invalid (dropping it from its form) even with the int-enum body the UI sends. Read-only via `soar legacy call settings/form-dynamic-parameters?formType=CloseCase`.

### `soar legacy call <op>`

Passthrough for integrations · ontology-mapping (selector read + batch upsert + body delete; the canonical raw-lane case) · environment-priorities · permissions/SSO (read-only by choice) · system/singleton settings · batch/bundle/selector surfaces.

---

## Threat Intelligence (Mandiant / Emerging Threats)

### `ti collections` / `collection <id>` / `related`

Mandiant `threatCollections` (campaigns/reports/actors/malware/vuln) — list (`collection_type:` filter + orderBy + `--limit`), get-by-id, related campaigns/reports for an IoC (`iocs related`, `threatCollections:fetchRelated`), and IoC match counts by collection alt name (`ti related`, `threatCollections:fetchIocMatchMetadata`). List/get are read-validated; related pivots are built + offline-tested. Read-only (upstream-sourced). Prefer v1 > v1beta > v1alpha; all three answer → pinned v1 (`tiAPIVersion`); threatCollections uses the project number.

### IoCs — `iocs find` / `iocs get` / `iocs related`

Modern IoC lookup, read-validated. `iocs find <value>` resolves indicators via the `fieldAndValue` body (`{value, valueType}`, type auto-detected for hash/domain/IP or `--type`), pinned v1; `iocs get <id>` fetches one record; `iocs related <ioc-id>` lists related campaigns/reports. SDK `FindIoCs`/`GetIoC`/`BatchGetIoCs` plus related IoC and threat-collection pivots (`chronicle/ti.go`); related pivots are built + offline-tested.

## Content Hub & integrations

### `soar marketplace list` / `get` / `contentpacks`

Content Hub reads (`soar/marketplace.go`) — `list [--installed]` (405 integrations) + `get <id>` + `contentpacks` (59). Read-validated.

Install/uninstall live-validated (Wave 11, `TestLiveMarketplaceInstallWriteSmoke` — install→verify→uninstall round-trip on an inert, not-installed utility pack, self-cleaning; reversible via the modern `:uninstall`). A single marketplace integration installs via `soar integration install --identifier <id>` (guarded; next row). Pack uninstall for a custom pack is CLI-reachable via `soar integration uninstall` (next row); the modern `marketplaceIntegrations:uninstall` itself stays SDK-only.

### `info soar-integrations`

Read-only runtime coverage report. Joins installed integration packs (`soar.ListIntegrations`) with legacy connector/job runtime cards (`ListConnectorCards`, `ListInstalledJobs`), groups aliases such as `<base>__<uuid>` / `productionIdentifier`, and flags `config_without_runtime`, `runtime_without_installed_pack`, `runtime_disabled`, and `unconfigured_runtime`. Built + offline-tested; no live mutation.

### `info cron`

Scans local scheduler-like files (`.github/workflows/`, `.gitlab-ci.yml`, `cron/`, crontabs, systemd units, etc.) for known `drift`, SIEM `push`, and SOAR `soar push` command references, then scans pulled `soar/jobs/` and `soar/playbooks/` JSON for scheduled SOAR automation (`cronSchedule`). `--host` adds current-user crontab/user-systemd inspection, and `--heartbeat-status <label>=<url>` HEAD-checks explicit read-only heartbeat status endpoints. Reports file:line references and labels only; no raw command or URL echo. Built + offline-tested.

### `soar build-playbook` / `soar playbook mold` / `soar playbook trigger set`

Composes a save-ready playbook JSON file from a full exported base playbook. Sets `trigger.cronSchedule` and can replace named placeholder steps with exported, already-wired integration-action step molds while preserving the base step graph identity. `soar playbook mold extract` extracts one exported action step; `mold apply` applies molds without requiring cron changes. `soar playbook trigger set` edits top-level enabled state and trigger fields in exported JSON. Offline-only; SOAR still validates the final body through `soar playbook validate` and `soar push playbook --dry-run` / save. Built + offline-tested.

### `soar integration scaffold` / `soar package-integration`

Scaffolds Python-backed custom action/job directories with JSON definition placeholders, then packages an already-shaped SOAR custom integration directory into a deterministic ZIP for IDE import. Refuses symlinks, skips VCS/OS junk, requires at least one JSON definition/manifest, and warns when no `.py` files are present. Built + offline-tested; no API call; SOAR validates the integration on import.

### `commands` / `surfaces` / `capabilities`

Machine-readable registries (no API call, no credentials, except `capabilities`'s live probe):

- `surfaces` maps the API families
- `commands` walks the cobra tree and lists every runnable verb with its path, kind (`read` vs `guarded-mutation`), and per-command `--json` support

**Rich flag schema (W73):** each `commands --json` flag now carries `{type, default, required, enum, usage}` plus the command's positional-arg spec and an example — so an agent builds a correct invocation first time instead of inferring a flag. Enum values are parsed from each flag's help text (angle-bracket placeholders stripped, ≥2-char tokens) so the catalog stays in lock-step with the help.

**`capabilities [--json|--offline]` (W73):** one session-bootstrap call fusing version + per-plane auth health (reuses `doctor`'s checks) + read-only state + a surface-status rollup (validated vs blocked), so an agent self-configures and avoids dead paths up front. The `json` flag is set from a `markJSON` annotation; build-vs-docs invariants in `internal/cli/wave62_test.go` / `wave73_test.go`. Live-validated (`capabilities --json` probes both planes + the surface rollup; `commands --json` carries the per-flag schema with enums). `capabilities` also carries a `skill_command` pointer so an agent discovers the embedded guide.

### `skill` / `skill install`

W84. The agent operating guide (`SKILL.md`) embedded in the binary via `//go:embed`, served by `skill` — `go install danny.vn/secops/cmd/secopsctl@latest` ships only the binary, not the repo's `skills/` tree, so an install-only agent has no other way to obtain the guide. `secopsctl skill` prints it (`--json` wraps `{name, description, body}`, frontmatter parsed); `secopsctl skill install [--dir <skills-dir>]` writes it to `<skills-dir>/secopsctl/SKILL.md` (default `$CLAUDE_CONFIG_DIR/skills` or `~/.claude/skills`) so an agent harness detects it as a first-class skill. Install is idempotent (a no-op when the file already matches) and refuses to overwrite a differing existing file without `--force`, so a hand-tuned copy is never clobbered silently. The embed package (`skills/secopsctl/skill.go`, `package skill`) lives beside the canonical `SKILL.md` so there is a single source. Offline, no credentials.

### structured `--json` errors + dry-run plan

W73, live-validated. A failed command under `--json` emits a structured `{code, message, retryable, status, request_id}` envelope on stderr (canonical google.rpc codes; `retryable` follows the transport retry policy; `*APIError`/`soar.Error` gain `Retryable()`), so stdout stays clean for the success/preview payload (a server 500 renders as `{code:"INTERNAL", status:500, retryable:false}`). `push --json` dry-run includes a per-object change plan (`items[]: {action, slug, server_id}`) via `reconcile.Summary.Changes`.

### `soar integration list` / `uninstall`

`list [--custom] [--json]` enumerates installed packs (`soar.ListIntegrations`); `uninstall --key <key>` deletes a custom pack (`soar.DeleteIntegration`, guarded). `soar.IsDeletableIntegration` = `custom:true` only — refuses commercial/installed packs. Read live-validated.

### `soar integration connector list` / `delete`

Connector definitions (templates inside a pack; distinct from the configured connector instances in the SOAR reconcile table). `list --integration <key>` (`soar.ListConnectors`); `delete --integration <key> --id <connId>` (`soar.DeleteConnectorDef`, guarded). Only `custom:true` definitions are deletable — removes a duplicated "Copy of …" connector without touching the stock one. Read + delete live-validated.

### `soar integration create` / `instances` / `configure` / `delete` (instances)

Integration instances are not reconcilable (no update endpoint; create body doesn't round-trip from any read) → imperative.

- `create --integration <id> --environment <env>` (new instance starts unconfigured/inert)
- `instances --integration <id>` lists configured instances (id · environment · name) via `GetOptionalIntegrationInstances` across `GetAvailableEnvironmentsForAgents`
- `configure --integration <id> --param key=value` reads current settings, overlays `--param` values (matched case-insensitively on `propertyName` or `propertyDisplayName`), and saves via `SaveStoreIntegrationConfigurationProperties` — secrets use `--param 'k=env:VAR'` (resolved at apply time, never on disk)
- `delete --integration <id>` resolves the full object (delete takes a body), auto-resolving `--id`/`--environment` from the integration's instances (single → auto; several → listed with copy-paste flags), and warns if playbooks use it

`TestLiveIntegrationInstanceCRUD` + instances/auto-resolve/configure live-validated (read + dry-run); guarded.

### `soar integration install` (+ pack `:install`/`:uninstall`)

Install a pack and its connector/job/action definitions. `soar integration install --identifier <id>` (guarded) installs a marketplace integration via the v1alpha `marketplaceIntegrations:install`. Legacy `legacy.GetPackageDetails` + `legacy.DownloadAndInstallIntegration` (`/api/external/v1/store/…` — not in the swagger snapshot, shape from the live Content-Hub request) install from the local store; the v1alpha `marketplaceIntegrations:install`/`:uninstall` is the documented twin — live-validated and cleanly reversible (install→uninstall round-trip leaves no residue).

Whole-integration delete is v1alpha-only (`integrations.delete`) and limited to genuinely custom packs (`custom:true`) — CLI-reachable via `soar integration uninstall`; per-tenant installed copies carry a `__<uuid>` suffix but are `custom:false`, so they are not whole-deletable. The legacy `/store` install path is the one with no clean reverse — prefer the modern `marketplaceIntegrations` pair.

### `soar integration action` / `job-def` (template/create/update/delete)

The IDE's Python-definition authoring loop (W60+W65):

- `template` fetches the new-definition skeleton (`actions:fetchTemplate`/`jobs:fetchTemplate`, read-only, Python scaffold included)
- `create` posts a filled body (`--file`, or `--name`/`--script` overlaid onto the template; guarded)
- `update` re-saves a complete modified body (guarded)
- `delete` removes by numeric id (guarded)

create = `POST integrations/{key}/{actions,jobs}`; update = `PATCH …/{id}?updateMask=<fields>` (sparse — a POST always creates, colliding on `displayName`, so updates go through PATCH by numeric id); delete = `DELETE …/{id}`. SDK `soar/authoring.go` (`Fetch*Template`/`Create*Def`/`Update*Def`/`Delete*Def`).

Live-validated by the gated `TestLiveAuthoringWriteSmoke`: create → update (in-place PATCH, description change observed) → delete for actions, create→delete for jobs (`job-def update` wired, not yet exercised).

---
