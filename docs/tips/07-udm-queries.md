# 07 · Search & UDM queries

UDM (Unified Data Model) is Chronicle's normalized event schema. A UDM query is a
filter expression over normalized fields like `metadata.event_type`,
`principal.user.userid`, and `target.application`. The `search` command group runs
ad-hoc searches against the live instance — UDM event search, raw-log search,
aggregations, single-event lookup, validation, and server-side saved searches.

Ad-hoc search is for *looking around* — it is **not** managed state. For the
repo-as-source-of-truth loop see [01-secops-as-code.md](01-secops-as-code.md). For
natural-language ("ask in English") search that compiles to UDM, see
[11-gemini-and-ai.md](11-gemini-and-ai.md). The one lesson that matters most here:
before you trust any rule that filters on a vendor tag, **verify those field values
in your own data** (below).

## The `search` command group

| Subcommand | Does |
|---|---|
| `search udm '<filter>'` | Run a UDM event search over a time window. |
| `search stats '<agg>'` | Run a `match:`/`outcome:` aggregation and print the result table. |
| `search raw '<regex>'` | Content-based raw-log search — print full raw lines matching a regex. |
| `search event <id>` | Inspect one event: enriched UDM (default), `--udm`, or `--raw` log. |
| `search validate '<query>'` | Validate a query's syntax without running it. |
| `search run --file <p>` | Run a UDM query loaded from a file (or `-` for stdin). |
| `search export '<filter>'` | Export ALL matching events to CSV (server-side; not capped at `--limit`). |
| `search saved …` | Server-side saved & shared searches (below). |

## Running a UDM search

```bash
secopsctl search udm 'metadata.event_type = "USER_LOGIN"' --hours 24
secopsctl search udm "$(cat examples/queries/login-failure.udm)" --hours 48
secopsctl search udm '<filter>' --from 2026-01-01T00:00:00Z --to 2026-01-02T00:00:00Z
```

Time-window and result flags (shared by `udm` / `run` / `saved run`):

| Flag | Default | Effect |
|---|---|---|
| `--hours N` | `24` | Relative look-back window ending now (used only when `--from` is unset). |
| `--from` / `--to` | now − hours / now | Absolute window (RFC3339 / ISO-8601); `--from` overrides `--hours`. |
| `--limit N` | `10000` | Caps results; a truncation warning goes to **stderr** so piped output stays clean. |
| `--all` | off | Return the **complete** result set via the search-view engine and report the **total match count** (not capped at `--limit`). |
| `--raw` | off | Print each matched event's full raw log line instead of the event summary. |

### Agent-first output: `--format`, `--fields`, `--out`

The read surface is built to pipe cleanly into a shell pipeline or an agent:

| Flag | Effect |
|---|---|
| `--format table\|json\|jsonl\|csv` | Output shape. Default: `table` on a terminal, `jsonl` when piped. The global `--json` is shorthand for `--format json`. |
| `--fields a.b,c.d` | Project only these dotted UDM field paths (e.g. `metadata.event_type,principal.hostname`) — narrows wide events to the columns you care about. |
| `--out FILE` | Write results to a file instead of stdout. |

```bash
# newline-delimited JSON, two projected columns, to a file
secopsctl search udm 'metadata.event_type = "NETWORK_DNS"' --hours 6 \
  --format jsonl --fields metadata.event_type,network.dns.questions.name --out dns.jsonl

# the complete result set + total match count, as CSV
secopsctl search udm 'principal.hostname = "host-01"' --all --format csv --out host01.csv
```

`.udm` files in [`examples/queries/`](../../examples/queries/) hold one filter each,
with `#`-prefixed comment lines. The loader strips the comments, so documentation can
live inline with the filter; run them with `search run --file <p>` or
`search udm "$(cat …)"`.

## Aggregations: `search stats`

A stats query carries a `match:` (group-by) and an `outcome:` (computed value) block.
`search udm` rejects an aggregation, so use `search stats` for them. This is also how
you validate a dashboard chart's query before authoring it
([06-dashboards.md](06-dashboards.md)):

```bash
secopsctl search stats --hours 24 'metadata.event_type = "USER_LOGIN"
match:
  principal.hostname
outcome:
  $count = count(metadata.id)
order:
  $count desc'
```

## Inspecting one event, raw search, validate, export

```bash
secopsctl search event 'AAAA…=' --json     # enriched UDM for one event id
secopsctl search event 'AAAA…=' --raw      # the original raw log line(s)
secopsctl search raw 'admin.*sudo' --hours 6 --unparsed   # regex over raw logs
secopsctl search validate 'metadata.event_type = "NETWORK_DNS"'   # syntax only, no run
secopsctl search export 'metadata.event_type = "NETWORK_DNS"' --hours 24 --out dns.csv
```

`search export` streams **all** matches server-side (not capped at `--limit`) to CSV
— use it for a full extract. Reach for `search udm --all` when you want the events
back in-process for shaping; `search validate` to check a query's syntax with no run;
`search event <id>` to pivot from a detection's event id to its enriched UDM or its
original raw log.

## Saved & shared searches (Search Manager)

Beyond local `.udm` files, the instance stores **server-side** saved searches —
visible in the console and optionally shared org-wide:

| Command | Kind |
|---|---|
| `search saved list` | read |
| `search saved get <id>` | read |
| `search saved run <id>` | read (same window/output flags as `search udm`) |
| `search saved save --name <n> (--query <udm> \| --file <p>) [--share]` | ⚠️ guarded |
| `search saved share <id>` / `search saved unshare <id>` | ⚠️ guarded |
| `search saved delete <id>` | ⚠️ guarded |

The mutating verbs (`save` / `share` / `unshare` / `delete`) are dry-run by default
and need `--yes`. Sharing a search makes it visible to the whole org, so review the
preview first. A proven query has two complementary homes: a **`.udm` file** in the
repo (diffable, code-reviewed, deployed with the rest of the config) and a
**server-side saved search** (discoverable in the console, shareable with analysts).

## Useful field anchors

A few fields do most of the work in detection and triage queries.

| Field | What it holds |
|---|---|
| `metadata.event_type` | Normalized event class (`USER_LOGIN`, `USER_CHANGE_PERMISSIONS`, `USER_CREATION`, `USER_RESOURCE_UPDATE_PERMISSIONS`, …). |
| `metadata.product_event_type` | The **vendor's** native operation name (e.g. `SetIamPolicy`, a token-issuance op). More precise than `event_type`. |
| `metadata.vendor_name` / `metadata.product_name` | Source product tags. **The dangerous ones to assume — see the warning below.** |
| `metadata.log_type` | Ingestion log type (which parser/feed produced the event). Often the most reliable discriminator. |
| `security_result.action` | `ALLOW` / `BLOCK` — pairs with `event_type` to split success vs. failure. |
| `principal.user.userid`, `principal.ip`, `target.application` | The actor, source IP, and acted-on app. |

Common shapes (tenant-neutral; see the examples directory for the full files):

```text
# Successful logins
metadata.event_type = "USER_LOGIN" AND security_result.action = "ALLOW"

# Failed logins (group by principal.user.userid in the UI to spot spray)
metadata.event_type = "USER_LOGIN" AND security_result.action = "BLOCK"

# IAM policy change
metadata.event_type = "USER_RESOURCE_UPDATE_PERMISSIONS"
AND metadata.product_event_type = "SetIamPolicy"
```

## ⚠️ The important lesson: verify vendor/log tags in YOUR data

> **Before trusting any curated (or third-party) rule that filters on a vendor tag
> like `metadata.vendor_name = "<Some Product>"`, confirm your events actually carry
> that exact value.** Sample your own data with a UDM query. A rule whose vendor
> filter never matches **silently never fires** — no error, no alert, no log line.
> It looks enabled and does nothing.

This is easy to hit: you enable a vendor's curated rule set whose display names
describe exactly the threat you care about, while every rule in it filters on a
`vendor_name`/`product_name` your ingestion never emits — because you license a
*different but adjacent* product, or your logs arrive through a connector that
normalizes to a different tag.

### A worked, generalized example

An org enables a curated rule set built for **Product A** (a full
productivity/collaboration suite) but actually runs only the **identity tier** of
that vendor's stack. The identity events flow in through the cloud-audit-log
pipeline and normalize with:

- `metadata.vendor_name = "<Cloud Platform vendor>"`
- `metadata.log_type   = "<CLOUD_AUDIT log type>"`

They are **never** tagged `metadata.vendor_name = "<Product A>"`. So every curated
rule that begins `metadata.vendor_name = "<Product A>"` matches zero events. The
dashboard shows the rules enabled; detection coverage for that surface is nil.

The remedy, end to end:

```mermaid
flowchart TD
  start([curated rule filters on a vendor tag]) --> sample["sample YOUR data: search udm per scenario<br/>(login ok/fail, admin action, token, IAM change)"]
  sample --> record["record real vendor_name · product_name<br/>event_type · product_event_type · log_type"]
  record --> match{"rule's filter matches<br/>your values?"}
  match -- yes --> keep["keep the curated set enabled"]
  match -- "no, but threat applies" --> custom["write a tenant-native custom rule<br/>on the confirmed fields"]
  custom --> disable["disable the dead curated set<br/>so the inventory reflects reality"]
  match -- "no, threat n/a" --> disable
```

1. **Discover the true shape.** Run `search udm` per scenario (login success, login
   failure, admin action, token issuance, IAM change) and record the actual
   `vendor_name`, `product_name`, `event_type`, `product_event_type`, and `log_type`
   your events carry. Trap: a `product_name` can be a *legacy* label for one event
   class and a different label for another, so it is a poor discriminator — prefer
   `product_event_type` and `log_type`.
2. **Build a compatibility view.** For each curated rule you rely on, check whether
   its filters align with your real values:

   | Filter the curated rule uses | Fires on your data? |
   |---|---|
   | `vendor_name = "<a vendor you actually emit>"` | Yes |
   | `vendor_name = "<a vendor you never emit>"` | **No — silent** |
   | `log_type = "<a log type you ingest>"` | Yes |
   | `product_name = "<an app/service you don't license>"` | **No** |

   Where a rule won't fire but the *threat scenario still applies to events you do
   have*, write a tenant-native custom rule on the fields you confirmed (see
   [03-yara-l-rules.md](03-yara-l-rules.md)), then disable the dead curated set.

### Don't hand-edit curated rules to fix this

You cannot edit curated rules to swap the vendor filter — they are Google-managed
and only *toggleable* at the rule-set level ([05-curated-rules.md](05-curated-rules.md)).
The remedy is always: confirm with `search udm`, replace with a custom rule on the
real fields, disable the dead set.

## Saving reusable queries

Keep proven filters as `.udm` files (one filter + a `#` docstring) so they are
diffable and re-runnable with `search run --file`, and promote the ones the whole team
should see to a **server-side saved search** (above). A small library — "who logged
in, from where," "failed logins by user," "admin actions," "IAM changes," "token
issuance" — pays for itself every investigation and doubles as the verification
toolkit for the lesson above. Treat these as ad-hoc tooling, distinct from the managed
rule/list/table state the repo deploys.

For natural-language search that compiles straight to UDM — and how to drive it
safely as an agent — see [11-gemini-and-ai.md](11-gemini-and-ai.md).
