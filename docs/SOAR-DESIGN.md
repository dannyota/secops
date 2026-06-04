# SOAR & legacy design — Wave 3

Design for the Google SecOps **SOAR** surface and the **legacy** endpoints, ported
from real operational SOAR usage. The governing principle:

> **Quarantine legacy.** Anything that exists only until a modern API lands goes
> in its own subpackage/file with a stated removal trigger, never mixed with the
> durable modern code. When the new API ships you delete a directory and the
> compiler tells you what to rewire.

All identifiers here are placeholders (`<tenant>`, `<num>`, `<reg>`, `<id>`) — the
public repo stays tenant-neutral; real values come from config/env at runtime.

## The three tiers

SOAR rides one host (`https://<tenant>.siemplify-soar.com`) with **one AppKey and
no ADC**, but spans three tiers with different removal triggers:

| Tier | Surface | Transport | Remove when… |
|---|---|---|---|
| ✅ **Modern** | v1alpha native: integrations · connectors · jobs · alertGroupingRules · moduleSettings · cases | `/v1alpha/projects/<num>/…/instances/<id>` + `?format=camel` + `x-goog-api-version` + `updateMask` | keep — long-term |
| 🟠 **Bridge** | `legacyPlaybooks:legacy*` (list/get/save/attach/stats) | v1alpha host, **legacy op names** | native v1alpha **playbook CRUD** ships |
| 🗑 **Legacy** | Siemplify external API: cases-queue bulk-close, AddComment/Tag/ChangePriority, GetCaseFullDetails, playbook Export/Import | `/api/external/v1/…` (offset paging) | v1alpha **bulk-case + playbook** endpoints ship |

Plus one **legacy SIEM** pair on the Chronicle side (ADC auth, not SOAR):
`legacy:legacyFindRawLogs` and `legacy:legacyBatchGetCases` (the SOAR-integer-id
⇄ SIEM-uuid bridge).

## Package layout

```
danny.vn/secops/
│
├── auth/                         OAuth(ADC)  +  APIKey/SOARAppKey      ← unchanged
├── config/                       + soar_url (tenant SOAR host)         ← small add
│
├── chronicle/   (SIEM · v1alpha · MODERN, ADC)
│   └── legacy.go   🗑 QUARANTINE FILE  ── legacyFindRawLogs, legacyBatchGetCases
│                   (SOAR int-id ⇄ SIEM uuid map). Delete when v1alpha equivalents land.
│
└── soar/   (host=https://<tenant>.siemplify-soar.com · AppKey, NO ADC)
    │
    │   internal/transport/        shared, durable plumbing (AppKey + host)
    │     • v1alphaDo()  → /v1alpha/projects/<num>/locations/<reg>/instances/<id>/…
    │                      auto: ?format=camel · x-goog-api-version · updateMask · {items,nextPageToken}
    │     • externalDo() → /api/external/v1/…      offset paging {requestedPage,pageSize}
    │
    ├── ✅ MODERN — v1alpha native (the keeper) ───────────────────────────────
    │     client.go        SOAR client
    │     integrations.go  integrations · connectors · jobs   (discovery)
    │     connectors.go    connectorInstances   GET · PATCH(updateMask) · :fetchLatestDefinition
    │     jobs.go          jobInstances         GET · PATCH(updateMask)
    │     grouping.go      alertGroupingRules · moduleSettings(:batchUpdate)
    │     cases.go         cases   (v1alpha listing)
    │
    └── 🗑 soar/legacy/   ── QUARANTINE SUBPACKAGE: delete the dir, modern is untouched ──
          doc.go               removal triggers per tier
          ─ TIER 2 · BRIDGE (v1alpha transport, legacy op names) ─
          playbooks.go         legacyPlaybooks:legacy{List,Get,GetByName,Save,Attach,Stats}
                               gotchas baked in (see below)
          ─ TIER 3 · LEGACY Siemplify external API (/api/external/v1) ─
          cases.go             GetCaseCardsByRequest · ExecuteBulkCloseCase ·
                               AddCaseComment · AddCaseTag · ChangeCasePriority · GetCaseFullDetails
          playbooks.go         GetEnabledWFCards · ExportWorkflowWithBlocksByIdentifier

   dependency rule:  soar  →  soar/internal/transport  ←  soar/legacy
                     (modern NEVER imports legacy; both share transport; legacy is a clean cut)
```

## Wire shapes actually sent — modeled as types

```
legacy/cases.go     CaseQueueRequest{ SortBy, RequestedPage, PageSize, Statuses[] }   // 1=OPEN 2=CLOSED
                    BulkCloseRequest{ CasesIDs[], CloseReason, RootCause, CloseComment, DynamicParameters[] }
                      └ CloseReason enum: 0 NotMalicious · 1 Malicious · 2 Maintenance · 3 Inconclusive
connectors/jobs     Parameters map[string]string   // EVERYTHING is a string ("true","100")
                      └ secrets read back as "***…" → pass through unchanged on PATCH (never re-send a real secret)
transport (v1alpha) every request: ?format=camel  +  header x-goog-api-version: v1alpha  +  PATCH ?updateMask=a,b
bridge/playbooks    coercePlaybookTypes(): id/priority/version/*UnixTimeInMs int→str (top-level, trigger, each step)
                    validatePlaybookName(): allow [A-Za-z0-9 _-], reject . ( ) [ ] : /
                    playbook save mints a NEW UUID → never cache it; re-resolve by display name
                    save = whole-body replace (not a patch): read → modify same body → save
```

## Gotchas to encode (verified against real usage)

- **Playbook UUID rotation** — every save mints a new identifier; the one you sent
  goes stale. Always re-resolve by display name after a save.
- **Playbook type coercion** — GET returns ints, save requires strings for
  `id`/`priority`/`version`/`*UnixTimeInMs` (top-level, `trigger`, each `step`);
  `templateName` must be `""`, never `null`.
- **Playbook name charset** — letters/digits/space/`-`/`_` only; reject `.()[]:/`.
- **Dual case IDs** — SOAR uses integer IDs, SIEM uses UUIDs; map via
  `legacy:legacyBatchGetCases` (`soarPlatformInfo.caseId`).
- **Parameters are always strings** on connectors/jobs (even bool/int); secrets
  read back masked (`***…`) and must be passed through unchanged on PATCH.
- **Integration clones** — integrations can appear twice (`Name` + `Name__<uuid>`);
  use the un-suffixed one for live edits.
- **Two paginations** — legacy is offset (`requestedPage`/`pageSize`); v1alpha is
  Google-style (`pageToken`/`nextPageToken`).

## Repo touchpoints beyond the SDK

- **config:** add `soar_url` (tenant SOAR host); reuse `project_number`/`region`/
  `customer_id` for the v1alpha path. AppKey via `auth.SOARAppKey` /
  `SECOPS_SOAR_APP_KEY` (no ADC).
- **internal/mirror + internal/cli:** `pull soar` (cases · playbooks · connectors ·
  jobs · grouping → YAML/JSON snapshots) and guarded `push` (bulk-close ·
  connector-patch · job-patch · playbook-save) under the same dry-run / LIVE-DEPLOY
  guard as rules.
- **leak guard:** SOAR snapshots carry masked secrets + integer case IDs; the
  pre-commit scanner already covers AppKey/secret patterns.

## Out of scope (per project decision)

No SentinelOne, no Teams/chat notifications — out of this repo's scope.
