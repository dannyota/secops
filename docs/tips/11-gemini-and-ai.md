# 11 · Gemini & AI

SecOps ships **Gemini** — a natural-language layer over the same UDM search engine
covered in [07-udm-queries.md](07-udm-queries.md), plus a security assistant. The
`gemini` command group exposes three verbs, all **read-only**:

| Command | Does | Runs the query? |
|---|---|---|
| `gemini generate '<NL>'` | Translate natural language to a UDM query. | No — prints the query only. |
| `gemini search '<NL>'` | Translate natural language to UDM **and** run it. | Yes. |
| `gemini ask '<question>'` | Ask the SecOps Gemini assistant a free-form question. | n/a — assistant answer. |

They build on the search surface in [07-udm-queries.md](07-udm-queries.md) and the
agent-driving discipline in [10-llm-and-automation.md](10-llm-and-automation.md).

## One-time opt-in

Gemini needs a one-time, per-account opt-in before it will answer. Enable it once:

```bash
secopsctl gemini ask --opt-in 'what can you help me investigate?'
```

After that the account is opted in and every `gemini` verb works without the flag.

## NL → UDM, two ways

The two query verbs differ only in whether they *run* the result:

```bash
# 1. generate — review-first: print the UDM, run nothing
secopsctl gemini generate 'failed logins to admin accounts in the last hour'

# 2. search — generate AND run, with the full search output flags
secopsctl gemini search 'network connections to a public IP in the last hour' \
  --format jsonl --fields metadata.event_type,target.ip --limit 500
```

`gemini search` accepts the same agent-first output flags as `search udm`
(`--format table|json|jsonl|csv`, `--fields` dotted UDM paths, `--out`, `--limit`,
`--hours`) — so once the model has produced a query, the result shaping is identical
to a hand-written search.

### The model picks the time window

When the natural language implies a window — "in the **last hour**", "**yesterday**",
"over the **past 7 days**" — Gemini infers it and that inferred window **wins over
`--hours`** (the flag is ignored in that case). Phrase the time range in the prompt
when you want a specific window; fall back to `--hours` only when the prompt says
nothing about time.

## The assistant: `gemini ask`

`gemini ask` is free-form Q&A against the SecOps assistant — explaining a detection,
suggesting an investigation pivot, summarizing a technique. It is read-only and
returns prose, not a query:

```bash
secopsctl gemini ask 'what does the NETWORK_DNS event type capture, and how do I hunt for DNS tunneling?'
```

## Driving Gemini safely (especially as an agent)

Natural-language generation is powerful and **non-deterministic** — the same prompt
can yield a different query run to run. The verification lesson in
[07-udm-queries.md](07-udm-queries.md) applies doubly: a syntactically valid
generated query can filter on a `vendor_name`/`log_type` your data never carries and
**silently return zero matches**, which an agent reasoning from the prompt alone will
read as "no activity" rather than "wrong field."

Practical habits:

- **Prefer `gemini generate` → review → run** over `gemini search` for autonomous
  work. Read the produced UDM, confirm it targets fields your events actually
  populate (`search udm`/`search stats` against a known anchor), *then* run it — or
  hand it to `search run --file`.
- **Pin what you trust.** Once a generated query proves correct, save it as a `.udm`
  file or a server-side saved search ([07-udm-queries.md](07-udm-queries.md)) and run
  *that* on a schedule. A standing job should run a reviewed, deterministic query, not
  re-generate one from NL each time.
- **Parse structured output.** Use `--format jsonl` (or the global `--json`) when an
  agent consumes `gemini search` results, exactly as with `search udm`.
- **All three verbs are reads.** They start a generation but create no managed
  artifact, so they remain available in hard read-only mode
  ([10-llm-and-automation.md](10-llm-and-automation.md)). Gemini is an investigative
  aid — verdicts and queries it produces are a second opinion, never a substitute for
  the dry-run-first review gate on anything that mutates state.

Cross-references:

- deterministic search, field anchors, the vendor-tag lesson — [07-udm-queries.md](07-udm-queries.md)
- how an agent drives the CLI (read-only mode, dry-run-first, output contract) — [10-llm-and-automation.md](10-llm-and-automation.md)
- the pull → review → push loop — [01-secops-as-code.md](01-secops-as-code.md)
