# Gemini

Ask SecOps in plain English. The `gemini` group wraps the console's
AI features: turn a natural-language question into a UDM query, run that query,
or ask the assistant for help. Deterministic, hand-written search lives under
[`search`](search.md) — `gemini` is the natural-language front door to the same
engine.

> 🔒 Every verb here is **read-only**. `gemini generate` does not even run a
> search (it only prints the query); `search` and `ask` only read. None of them
> deploy or mutate the tenant, so they all run unchanged under a hard read-only
> session (`--read-only` / `SECOPS_READONLY=1`).

Gemini runs on the **Chronicle SIEM plane** (ADC / OAuth auth), like
[`search`](search.md). Make sure `secopsctl doctor` is green for the SIEM plane
before using it.

## 🧭 The three verbs

| Command | Runs the query? | Does |
|---|---|---|
| `gemini generate <nl>` | no | translate natural language → a UDM query and **print it** (plus any time window the model inferred) |
| `gemini search <nl>` | yes | translate → UDM **and run it**, with the full search output contract |
| `gemini ask <question>` | n/a | ask the assistant a question (YARA-L help, UDM fields, grounded answers) |

The split is deliberate: `generate` is the **review-first** path — you read the
query before anything runs — while `search` is the one-shot path that also
executes. For automation, prefer `generate`, inspect the query, then run it with
[`search udm`](search.md) (or `gemini search` once you trust the phrasing).

## ✍️ generate — natural language → query

```bash
secopsctl gemini generate 'failed logins to admin accounts in the last hour'
```

Prints the UDM query Gemini produced on stdout. If the question names a time
window ("…in the last hour"), the **model's suggested window** is printed to
stderr as `suggested window: <start> … <end>` so you can carry it into a run.
Add `--json` to get the structured result (query + inferred time range).

```bash
# Capture just the query text (stderr carries the suggested window separately).
q=$(secopsctl gemini generate 'dns lookups to newly seen domains today')
secopsctl search udm "$q" --hours 24
```

`generate` only has the global `--json` flag — it does not run a search, so it
takes no window or output flags.

## 🔎 search — translate and run

```bash
secopsctl gemini search 'network connections to a public IP in the last hour'
```

Translates the question to UDM and runs it in one step. Results use the same
[output contract](search.md#-output-shape-the-result) as `search udm`.

| Flag | Default | Purpose |
|---|---|---|
| `--hours int` | `24` | look-back window in hours — **ignored when the model infers a window** from the text |
| `--limit int` | `1000` | maximum number of events to return |
| `--format string` | auto | `table` (terminal) / `jsonl` (piped) / `json` / `csv` |
| `--fields string` | — | comma-separated dotted UDM paths to project |
| `--out string` | stdout | write results to a file instead of stdout |

**Window precedence:** an explicit `--hours` (or `--from`) wins; otherwise the
model's suggested range is used when it inferred one; otherwise it falls back to
the last `--hours` (default 24). So `'…in the last hour'` is honored as a
one-hour window without any flag, but `--hours 6` overrides the model.

```bash
# Model infers the window from the phrase; project two fields as JSONL.
secopsctl gemini search 'blocked logins in the last 6 hours' \
  --fields principal.user.userid,security_result.action --format jsonl

# Force a 24h window regardless of the phrasing.
secopsctl gemini search 'powershell process launches' --hours 24
```

## 💬 ask — the assistant

```bash
secopsctl gemini ask 'how do I match a parent process in YARA-L?'
```

Asks the SecOps Gemini assistant — YARA-L authoring help, UDM field questions,
environment-grounded answers. It returns prose (rendered from the assistant's
text / HTML), any code blocks, and a reference count (`--json` for the full
structure). It is read-only: it answers, it changes nothing.

## 🔑 One-time opt-in

A SecOps account must be **opted in to Gemini once** before the assistant
answers. The opt-in is a one-time, per-account enablement — run it via
`gemini ask --opt-in`, optionally with a question in the same invocation:

```bash
secopsctl gemini ask --opt-in 'what UDM field holds the source IP?'
```

If the account is not opted in, `ask` fails with a clear message telling you to
re-run once with `--opt-in`. After that the enablement sticks for the account —
you do not pass `--opt-in` again, and `generate` / `search` work without it.

## 🤖 Note for automation

These verbs read; they never create artifacts, so a hard read-only session does
not block them. The AI features that **do** create server-side artifacts — for
example `soar playbooks generate` and the case-simulation generators — are the
ones a read-only session refuses (they degrade to a dry-run preview). When an
agent should never write, set `SECOPS_READONLY=1`: `gemini` keeps working while
those generators are held back. Review a generated query with `generate` before
letting anything run, and treat the model's output as a draft to verify, not a
fact.

## 🔗 See also

- [Search](search.md) — the deterministic search engine these verbs feed.
- [Triage](triage.md) — where `gemini` fits in the alert → case → rule flow.
- [LLM & automation](../tips/10-llm-and-automation.md) — driving the AI surfaces safely.
- [Catalog (SIEM)](../design/catalog-siem.md) — per-surface status.
