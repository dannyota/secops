# Gotchas — non-obvious operational traps

Things the per-command `--help` cannot express. The CLI handles most of these
transparently, but an agent or automation driving secopsctl needs to know them.

## A 500 is usually wrong-host, not a broken endpoint

The same v1alpha paths are served by two hosts. SOAR-flavored surfaces (cases,
Content Hub, connectors) **500 on the chronicle host** and work on the SOAR
host; SIEM surfaces (rules, iocs, riskConfig) **404 on the SOAR host**. The CLI
routes correctly, but before declaring a surface broken, try `--legacy` (forces
the legacy AppKey path) or `soar legacy call` as an escape hatch. A generic
`INTERNAL` 500 is almost always a shape bug (a `null` where the server expects
`[]`) or wrong host, not a real outage.

## Base64 event IDs need URL-safe encoding

`search event <id>` takes the base64 `metadata.id` from a search result. The
enriched path needs URL-safe, unpadded base64 — the CLI converts it
automatically; just pass the id verbatim from `search udm --json`.

## --prune deletes server-only objects

`push <target> --prune` deletes live objects with no local file. Off by default;
requires a fresh pull this session. Not every surface is prune-eligible — check
with `status surfaces` (PRUNE column) or `push <target> --help`.

## Playbook UUIDs rotate on save

Every save of a SOAR playbook mints a new `identifier`. Resolve playbooks by
name (`--name`), not by identifier, and re-read the list after any save.

## Write-then-list lag; a failed write may have applied

A write can **return an error yet still persist** the object — verify with
get/list after a failure, never assume the error means nothing happened.
Create-then-list has indexing lag while deleted ids tombstone. Give throwaways
unique self-identifying names (e.g. `secopsctl-smoke-<nanos>`) and **delete by
exact id**, never a list sweep.

## Never retry a mutating POST that 500s

A non-idempotent POST that returns 500 may have already applied server-side.
The transport retries 5xx only for idempotent methods (GET, PUT, DELETE) and 429
for any method. On a write 500, diff your request against the real UI request
(browser dev-tools, Network tab) before concluding the endpoint is broken — the
gap is usually an omitted-vs-empty collection or a value the swagger wrongly
calls optional.

## Stats query gotchas

A `match:` section takes a bare field (`metadata.log_type`), not an assignment.
Do not name a `match:`/`outcome:` variable with a reserved YARA-L keyword
(`$rule`, `$events`) — it compiles but fails at execute time. `charts add`
warns; `dashboards verify` rewrites the opaque 400 into an actionable message.
