# 💡 08 · Feeds & parsers

Detection is only as good as ingestion. **Feeds** bring raw logs into SecOps;
**parsers** normalize those raw logs into UDM. This doc covers tracking both as
code: the ingest-health check that catches most "why isn't this alerting?"
problems, the rule that secrets never enter the repo, and the practical notes on
parser files.

For the overall loop see [01-secops-as-code.md](01-secops-as-code.md); for how a
broken vendor tag silently kills detection *downstream* of ingestion, see
[07-udm-queries.md](07-udm-queries.md).

```mermaid
flowchart LR
  src[("source<br/>API · GCS · webhook")] -- "credential" --> feed["feed<br/>state"]
  feed -- "raw log" --> parser["parser (CBN)<br/>GOOGLE / CUSTOMER"]
  parser -- "UDM event" --> det["rules · dashboards · UDM search"]
  feed -. "state: FAILED → starved" .-> det
```

## Feeds

A feed is one ingestion source: an API poller, a cloud-storage bucket, a webhook,
etc. `secopsctl pull feeds` writes one YAML per feed capturing:

| Field | What it carries |
|---|---|
| `display_name`, `name`, `uid` | identity and server resource name |
| `source_type` | `API`, `GOOGLE_CLOUD_STORAGE`, … |
| `log_type`, `asset_namespace`, `labels` | routing and tagging |
| `settings` | source-specific config (secrets redacted) |
| `state` | `ACTIVE` · `RUNNING` · `FAILED` · `EMPTY` … |
| `failure_msg`, `failure_details` | why a `FAILED` feed is down |

### `state: FAILED` is the first ingest-health check

When detection looks wrong — a rule that should fire is silent, a dashboard goes
flat, an investigation finds no events — **check feed `state` before anything
else.** A feed in `state: FAILED` is not ingesting; everything downstream of it
(parsers, UDM, rules, dashboards) is starved of data, with no error at the
detection layer to tell you so.

```bash
secopsctl pull feeds
# the summary line tallies state, e.g. (state: {'ACTIVE': 3, 'FAILED': 1})
# then scan the pulled YAML for any feed whose state is FAILED
```

A failed feed usually carries a `failure_msg`. The most common is a credential
problem — an expired key, a rotated password, a revoked token → an auth/login
failure on the source side. Those are fixed **at the source or in the SecOps UI
feed config**, not by editing the repo YAML: the YAML mirrors live state; it is
not where credentials live (see next section).

> Make "`pull feeds` and grep for `FAILED`" a routine health check, not just a
> break-glass step.

### Secrets never live in the repo

Feed configs reference credentials, but **the credentials themselves never get
committed.** On `pull`, secret scalar fields are **redacted** with
`***REDACTED***` before the YAML is written — keys such as `password`, `secret`,
`apiKey`, `token`, `clientSecret`, `privateKey`, `authToken`, and
`secretAccessKey` (and their `snake_case` forms) come back masked. This means:

- A pulled feed YAML is safe to commit and review; it shows the *shape* of the
  config, not the secret values.
- On `push`, the real secret must be supplied at push time — on an **update**,
  `push feeds` overlays your local edits onto the live (unredacted) feed, so any
  scalar still holding the redaction marker keeps the live secret rather than
  clobbering it. On a **create**, there is no live secret to fall back to, so a
  body that still carries the marker is **refused** — you must replace the marker
  with the real secret before pushing.

Same discipline as everywhere else in the tool: auth comes from ADC or an env
token, and no API key, password, or service-account JSON belongs in version
control.

### Editing a feed: pull → edit → `push feeds`

`push feeds` reconciles the local feed YAML to live (create/update). The loop is
the same as everywhere else — edit the YAML, dry-run, review, apply:

```bash
secopsctl pull feeds                 # refresh local state first
# edit feeds/<feed>.yaml; supply the real secret in place of any ***REDACTED*** marker
secopsctl push feeds --dry-run       # preview the diff (LIVE DEPLOY banner)
secopsctl push feeds --yes           # apply for real
```

Feeds are **not prune-eligible**: deleting a feed stops ingestion, so a missing
local file never deletes a live feed. To stop a feed, do it explicitly in the
SecOps UI, not by removing the YAML. Credential failures (an expired key, a
rotated password) are still fixed at the source or in the UI feed config, not by
editing the redaction marker in the repo.

## Parsers

A parser turns a raw log line for a given log type into UDM. `secopsctl pull
parsers` writes the active parser source per log type **in use** — the puller
derives the set from your feeds' log types, so there's no point tracking parsers
for log types you don't ingest. Two files per log type:

| File | Contents |
|---|---|
| `parsers/<LOG_TYPE>.conf` | parser source (CBN — "Configuration-Based Normalization", the parser language), base64-decoded to plain text |
| `parsers/<LOG_TYPE>.yaml` | metadata: `parser_id`, `version`, `creator_source`, `state`, `release_stage`, `type`, `inactive_parser_count` |

### Prebuilt (Google-managed) vs. custom

The `creator_source` / `type` in the metadata tells you who owns the parser:

| `creator_source` | `type` | Meaning |
|---|---|---|
| `GOOGLE` | `PREBUILT` | Google maintains it. Track it to *see* what normalization you rely on; you generally don't edit these. |
| `CUSTOMER` | (custom) | Your tenant wrote/overrode it. **This is the file you edit and push** when normalization needs to change. |

If a field your rules depend on isn't populated in UDM, the parser is where you
look — but only a `CUSTOMER` parser is yours to change. For a prebuilt parser
that's mis-mapping, the fix is a parser override / extension, not editing
Google's source in place.

### Editing a parser: versioned, immutable, create-new-version + activate

Parsers are **versioned and immutable** — there is no in-place edit. `push
parsers` does **not** patch the live parser; it **creates a new parser version**
from your edited `.conf` and **activates** it. On `--yes`, live ingestion
switches to the new version. The previous version is left **INACTIVE** so a
rollback stays available, and the server mints a fresh parser id each time, so
`parser_id` in the companion YAML is **volatile** — it is rewritten on the next
`pull`. The loop:

```bash
secopsctl pull parsers               # refresh local state first
# edit parsers/<LOG_TYPE>.conf
secopsctl push parsers --dry-run     # preview the diff (LIVE DEPLOY banner)
secopsctl push parsers --yes         # creates + activates a NEW version; live ingestion switches
```

Like feeds, parsers are **not prune-eligible**: a missing local file never
deletes a live parser. (The active set is derived from your feeds' log types, so
a transient gap can't drive a deletion either.)

> **Caution: editing a prebuilt `.conf` creates a CUSTOMER override.** The
> reconcile update does not distinguish `creator_source`. If you edit a Google
> `PREBUILT` parser's `.conf` and push it, you create and activate a `CUSTOMER`
> parser that overrides Google's — live ingestion switches to your copy. Track
> prebuilt parsers to *see* the normalization you rely on, but don't edit a
> prebuilt `.conf` you only mean to track. Always review the `git diff` before
> `push parsers`.

### Large files — grep, don't open

Parser `.conf` files for high-volume log types can be **huge** (tens of
thousands of lines / multiple megabytes). **Grep them; don't open them casually**
in an editor or read them whole into a tool — it's slow and rarely what you want.
Search for the specific field mapping, log key, or UDM target you care about:

```bash
grep -n "principal.user.userid" parsers/<LOG_TYPE>.conf
grep -n "event_type"            parsers/<LOG_TYPE>.conf
```

Find the relevant block, understand the surrounding context, and make the
narrowest change. As with dashboards ([06-dashboards.md](06-dashboards.md)), the
file is large and structured — review the `git diff`, not the whole file, and
deploy only after a dry-run.
