# 12 · Parser extensions & CBN authoring

Parser extensions add fields to or override behavior of a prebuilt (Google-managed)
parser without replacing it. They use the same CBN (Configuration-Based
Normalization) syntax as full parsers but run **after** the base parser, merging
their output into the same UDM record. This doc covers the authoring patterns
that are non-obvious from the
[official docs](https://docs.cloud.google.com/chronicle/docs/event-processing/using-parser-extensions)
alone.

For feed and parser lifecycle basics see
[08-feeds-parsers.md](08-feeds-parsers.md); for the UDM event-type field
requirements see the
[UDM Usage Guide](https://docs.cloud.google.com/chronicle/docs/unified-data-model/udm-usage).
Starter CBN templates are in
[`examples/parser-templates/`](https://github.com/dannyota/secops/tree/master/examples/parser-templates)
(`EXTENSION_JSON.conf`, `EXTENSION_SYSLOG_KV.conf`, `EXTENSION_MULTI_BRANCH.conf`).

## Extension lifecycle

Extensions are **immutable** — there is no update-in-place. The deployment cycle
is: delete the old extension → create a new one → wait for VALIDATED state
(~15–30 min) → activate. All extraction from the old extension pauses during
validation, so batch changes into one deployment to minimize the gap.

```bash
secopsctl ingest parsers extension list <LOG_TYPE>
secopsctl ingest parsers extension delete <LOG_TYPE> <id> --yes
secopsctl ingest parsers extension create <LOG_TYPE> --cbn ext.conf --yes
# wait until state = VALIDATED (poll with extension list)
secopsctl ingest parsers extension activate <LOG_TYPE> <id> --yes
```

> One extension per log type is a hard platform limit.

## Testing with `parsers run`

`parsers run` evaluates a CBN against sample logs without creating or activating
anything — safe to iterate on during development.

```bash
secopsctl ingest parsers run <LOG_TYPE> --cbn ext.conf --logs samples.txt
secopsctl ingest parsers run <LOG_TYPE> --cbn ext.conf --logs samples.txt --json
secopsctl ingest parsers run <LOG_TYPE> --cbn ext.conf --logs samples.txt --statedump
```

A statedump filter is auto-injected when the CBN does not already contain one, so
diagnostics are always available. The statedump shows `@onErrorCount` (how many
`on_error` flags fired), `@output` (what the parser emitted), and all
intermediate variables. Three failure modes are surfaced:

| Failure | What you see |
|---|---|
| CBN pipeline error (missing field, bad syntax) | Full error message with filter index and field path |
| UDM validation (e.g. USER_LOGIN missing `target.user`) | `LOG_PARSING_GENERATED_INVALID_EVENT` with the missing field |
| Silent no-output (`on_error` absorbs everything) | Statedump with `@onErrorCount`, error flags, empty `@output` |

Use `--statedump` to see the full statedump on every log, including successful
ones — helpful for verifying which branches the parser took.

## Merge semantics

Extension output **merges with** the base parser UDM — it does not replace it.
Fields set by the base parser (principal, target, description, timestamps)
survive; the extension adds or overrides individual fields.

- Setting `metadata.event_type` in an extension **overrides** the base parser's
  event type. Use this to upgrade `GENERIC_EVENT` to a proper type (e.g.
  `STATUS_UPDATE`) by adding only the fields the new type requires.
- `additional.fields` labels from the extension merge seamlessly with base
  parser labels — they are a key-value set, not a list that overwrites.
- Repeated fields (e.g. `principal.ip`) follow the append/replace selector
  configured at extension creation time.

## CBN patterns

### Extracting nested JSON arrays with `split_columns`

```ruby
json { source => "message"  array_function => "split_columns"  on_error => "_not_json" }
```

After `split_columns`, deeply nested arrays are accessible via positional
dot-path indexing: `%{protoPayload.metadata.event.0.parameter.0.name}` reaches
the first element of the `event` array, then the first element of its
`parameter` array.

### JSON booleans require grok — dot-path extraction fails

CBN's `%{field.boolValue}` triggers `on_error` for both `true` and `false` —
boolean values cannot be extracted via dot-path. Workaround: grok on the raw
`message` field with a regex that handles both key orderings (JSON key order is
not guaranteed):

```ruby
grok {
  match => { "message" => "\"is_suspicious\"[^}]*\"boolValue\"\\s*:\\s*(?P<_val>true|false)" }
  on_error => "_try_reverse"
}
if [_try_reverse] {
  grok {
    match => { "message" => "\"boolValue\"\\s*:\\s*(?P<_val>true|false)[^}]*\"is_suspicious\"" }
    on_error => "_no_val"
  }
}
```

### Separate mutate blocks for nullable fields

Extracting multiple fields in one `mutate` block with a single `on_error` means
**all** extractions fail if **any** field is null. Split nullable fields into
separate blocks with independent error flags:

```ruby
mutate { replace => { "_service" => "%{protoPayload.serviceName}" }  on_error => "_no_svc" }
mutate { replace => { "_method"  => "%{protoPayload.methodName}"  }  on_error => "_no_method" }
```

Then gate per-block:

- Service-only branch: `if ![_no_svc] and [_service] == "..."`
- Both required: `if ![_no_svc] and ![_no_method] and ...`

### Method-based gating for shared structures

When multiple services share the same payload structure (e.g. `SetIamPolicy`
under `cloudresourcemanager` and `storage.setIamPermissions` under `storage`
both carry `policyDelta.bindingDeltas`), gate on method name instead of service
to share one extraction block:

```ruby
} else if ![_no_method] and ([_method] == "SetIamPolicy" or
                             [_method] == "storage.setIamPermissions") {
```

For method families (`firewalls.insert`, `firewalls.patch`, `firewalls.delete`),
use regex matching instead of listing each variant:

```ruby
if [_method] =~ "firewalls" {
```

### Array value concatenation

Arrays like `multiStrValue` need per-position extraction with fallback:

```ruby
mutate { replace => { "_v0" => "%{...multiStrValue.0}" }  on_error => "_no_v0" }
mutate { replace => { "_v1" => "%{...multiStrValue.1}" }  on_error => "_no_v1" }
if ![_no_v0] and ![_no_v1] {
  mutate { replace => { "_combined" => "%{_v0},%{_v1}" } }
} else if ![_no_v0] {
  mutate { replace => { "_combined" => "%{_v0}" } }
}
```

### JSON-escaped quotes in Cloud Logging

When logs arrive via Cloud Logging, the raw log is the JSON envelope. Quoted
key-value fields inside `jsonPayload.message` (e.g. syslog `devname="xxx"`)
appear as `devname=\"xxx\"` in the raw bytes — literal backslash-quote.

The standard `%{DATA:inner_msg}` grok **truncates at the first `\"`** because
`DATA` (`.*?`) is non-greedy and stops at the first quote. Fix: grok directly
on `message` with double-escaped backslash-quote:

```ruby
grok {
  match => { "message" => "devname=\\\\\"%{DATA:_devname}\\\\\"" }
  on_error => "_no_devname"
}
```

Escaping levels: CBN config `\\\\\"` → grok regex `\\"` → matches raw
bytes `\"`. This pattern applies to any Cloud Logging JSON carrying quoted
syslog key-value fields.

### Mixed log formats in one extension

One extension per log type is a platform limit. When a feed carries mixed
formats under one log type (e.g. both FortiGate syslog and a VPN appliance
prefixed with `vpn_log:`), the extension must handle all formats in one CBN:

```ruby
if [message] =~ "vpn_log:" {
  # VPN-specific extraction
  mutate { merge => { "@output" => "event" } }
} else {
  # default format extraction
  mutate { merge => { "@output" => "event" } }
}
```

Each branch has its own `@output` merge, so non-matching formats produce no
extension output and the base parser handles them unchanged.

## UDM validation

Each `metadata.event_type` requires specific related fields. Setting an event
type without its required fields produces a validation error:

```text
generic::unknown: invalid event 0: LOG_PARSING_GENERATED_INVALID_EVENT:
  "udm validation failed: target field is not set"
```

Common required fields by event type:

| `event_type` | Required fields |
|---|---|
| `USER_LOGIN` | `target.user` |
| `NETWORK_CONNECTION` | `target.hostname` or `target.ip` |
| `STATUS_UPDATE` | `principal.hostname` or `principal.ip` |
| `GROUP_CREATION` | `principal.user`, `target.group` |

See the
[UDM Usage Guide](https://docs.cloud.google.com/chronicle/docs/unified-data-model/udm-usage#required_and_optional_fields)
for the full matrix. When upgrading `GENERIC_EVENT` to a specific type, add the
required fields **before** setting `event_type`, or guard both in the same
conditional block.

## Error handling

Use `on_error` on every extraction that can fail, and test the flag before
proceeding. The
[official guidance](https://docs.cloud.google.com/chronicle/docs/event-processing/parser-tips-troubleshooting#handle_errors_in_parser_statements)
recommends `drop { tag => "TAG_UNSUPPORTED" }` for unrecognizable formats —
the raw log is still ingested and searchable via raw log search; only UDM
normalization is skipped.

```ruby
json { source => "message"  array_function => "split_columns"  on_error => "_not_json" }
if [_not_json] {
  drop { tag => "TAG_UNSUPPORTED" }
}
```

Available drop tags: `TAG_UNSUPPORTED`, `TAG_MALFORMED_ENCODING`,
`TAG_MALFORMED_MESSAGE`, `TAG_NO_SECURITY_VALUE`. The tag value appears in
the `drop_reason_code` field of the ingestion metrics BigQuery table.

## Statedump for debugging

The `statedump` filter shows the parser's full internal state at any point in
the pipeline — all variables, `@onErrorCount`, `@output`, and the `event`
object being built:

```ruby
statedump { label => "after-extraction" }
```

Output includes:

- `@onErrorCount` — total `on_error` flags that fired
- `@output` — the events queued for output (`[]` = nothing will be emitted)
- Every intermediate variable and its current value
- The `event` object with all UDM fields set so far

> `secopsctl ingest parsers run` auto-injects a statedump and enables
> `statedumpAllowed`, so diagnostics are always available without modifying
> the CBN. Remove any `statedump` blocks before submitting an extension for
> validation — the platform rejects extensions that contain them.
