# 02 · Architecture & the Hybrid Client

`secopsctl` talks to one Chronicle instance, identified entirely by a config
file — no tenant data is compiled in. This doc covers how the client is built,
the gotchas it works around, and how local files map back to live resources. For
the workflow these mechanics serve, see
[01-secops-as-code.md](01-secops-as-code.md).

## Configuration is the only source of identity

The instance is described by a small YAML config with four required keys:

```yaml
project_id:      your-project-id
project_number: "000000000000"
region:          us
customer_id:     00000000-0000-0000-0000-000000000000
```

`base_url` and `ui_url` default from `region` if omitted
(`base_url = https://<region>-chronicle.googleapis.com/v1alpha`). The config is
discovered in a fixed search order — an explicit `--config` flag, then the
`SECOPSCTL_CONFIG` env var, then `./config/instance.yaml`, then
`~/.config/secopsctl/instance.yaml`. Nothing reads identifiers from anywhere
else; there are no hard-coded project numbers or customer IDs in the code. Copy
`config/instance.example.yaml` (placeholders only) to start.

## Why a hybrid client (SDK primary + raw HTTP fallback)

The client prefers Google's official `secops` SDK and falls back to raw HTTP only
where it must.

**Default to the SDK.** It handles auth, pagination, retries, and the
request/response shapes for most surfaces — rules, reference lists, data tables,
rule deployments, curated deployments, UDM search. Use it for everything it
covers.

**Fall back to raw HTTP** (`raw_get` / `raw_post` / `raw_patch`, thin `urllib`
helpers with a Bearer token) only when:

- the SDK lacks the endpoint (e.g. the native-dashboard listing, certain curated
  rule-set category listings, a legacy raw-log lookup);
- the SDK's signature churned between versions (it is pre-1.0 and shifts shape
  between minor releases) — when uncertain, prefer raw and leave a comment;
- you need a beta endpoint the SDK has not wrapped yet.

Keep the raw helper small on purpose. When the SDK catches up to an endpoint,
migrate the raw call back to the SDK.

**Lazy SDK import.** The SDK is imported *inside* `get_chronicle()`, not at module
load. This keeps the package importable — and `secopsctl --help`, config
loading, and the offline test suite working — on a machine that does not have the
`secops` SDK installed. Build CLI handlers the same way: import the heavy modules
inside the handler, not at the top of the file.

**Auth.** A `SECOPS_ACCESS_TOKEN` env var overrides everything; otherwise the
token comes from `gcloud auth application-default print-access-token` (ADC). No
service-account JSON in the repo, ever.

## The numeric-vs-string project gotcha

This is the single most surprising thing about the Chronicle REST API. Different
endpoints build the resource `name:` with either the project **ID** (a string
like `your-project-id`) or the project **number** (digits like `"000000000000"`).
There is no way to know which from the docs alone — you learn it per endpoint.

| Endpoint family | Project form |
|---|---|
| `rules`, `nativeDashboards`, `curatedRuleSetCategories/*`, legacy raw-log lookup | **numeric** (project number) |
| `referenceLists`, `dataTables`, `feeds` | **string** (project ID) |

The `raw_*` helpers take a `numeric_project=True` flag to switch which form goes
into the path. **Heuristic:** if a URL you construct returns `404`, try the other
form before assuming the resource is missing. The instance path is
`projects/<id-or-number>/locations/<region>/instances/<customer_id>`, with the
`<id-or-number>` selected by that flag.

## etag — optimistic concurrency

Chronicle returns an `etag` on every mutable resource. `pull` stores it in the
companion YAML; every `push` round-trips the stored `etag` back to the server. If
someone edited the resource in the UI since your last pull, the server's `etag`
will not match yours and the write is rejected. That rejection is a *feature* —
surface it as a clean error so the operator re-pulls and re-reviews, never a
silent overwrite of a concurrent edit. Honoring `etag` is how the
pull-before-edit rule in [01-secops-as-code.md](01-secops-as-code.md) is enforced
at the API level. Not every entity carries a local `etag` (cases and playbooks,
for instance — see [09-soar-operations.md](09-soar-operations.md)); for those,
coordination is manual.

## Slug filenames vs. server IDs

The server identity for a rule is a UUID-ish string (e.g. `ru_<uuid>`); for a
human the obvious handle is the display name. `secopsctl` writes files named with
the **slugified display name** and stashes the real server ID (`rule_id`,
dashboard name, etc.) plus the `etag` in the companion YAML/JSON.

- **Pro:** filenames diff cleanly in git and are easy to find by name.
- **Con:** renaming a rule's display name changes its slug, which on push looks
  like *delete the old + create the new*. So **don't rename slug files casually**
  — rename only when you genuinely intend to rename the live entity, and expect a
  delete-and-recreate. Never rely on the filename alone to round-trip a resource;
  the companion YAML holds the truth. (`slugify()` lowercases, replaces
  non-alphanumerics with hyphens, and collapses runs.)

Entity-specific conventions build on this: rules in
[03-yara-l-rules.md](03-yara-l-rules.md), reference lists and data tables in
[04-reference-lists-data-tables.md](04-reference-lists-data-tables.md), curated
rules in [05-curated-rules.md](05-curated-rules.md).

## Optional: forcing IPv4 on broken-IPv6 VPNs

Some corporate VPNs and Context-Aware-Access setups have unreliable IPv6 routing
to `*.googleapis.com`, which shows up as intermittent ADC reauth prompts and
connection resets. `secopsctl` ships an opt-in workaround: a `force_ipv4()`
helper monkeypatches `socket.getaddrinfo` to drop `AF_INET6` results (with a
fallback to the original lookup for hosts that have no IPv4 record at all).

It is **off by default.** It activates only when `SECOPS_FORCE_IPV4` is set to
`1`, `true`, or `yes` (`maybe_force_ipv4()` checks the env at client load). On a
healthy network you never need it; on a VPN where googleapis.com over IPv6 hangs,
set the env var and the problem disappears. The patch is idempotent — calling it
twice is harmless.
