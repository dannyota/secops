# secopsctl tips

Practical, tenant-neutral tips for operating **Google SecOps (Chronicle
SIEM + SOAR)** as code. These notes share the *craft* — the workflow, the API
gotchas, the entity conventions, and the safety habits — without any tenant's
identifiers or data. They pair with the `secopsctl` CLI but are handy on their
own.

Start with [01](01-secops-as-code.md) and [02](02-architecture-client.md); the
rest you can read in any order.

| Doc | Summary |
|---|---|
| [01 · SecOps as Code](01-secops-as-code.md) | The pull → review → push loop; why `pull` is read-only and every `push` is a live deploy. |
| [02 · Architecture & the Hybrid Client](02-architecture-client.md) | Config-as-identity, SDK-primary/raw-HTTP-fallback client, the numeric-vs-string project gotcha, `etag`, slugs, optional IPv4 forcing. |
| [03 · YARA-L Rules](03-yara-l-rules.md) | Custom detection rules: `.yaral` + companion `.yaml`, creating vs. editing, deployment state, what `push` mutates. |
| [04 · Reference Lists & Data Tables](04-reference-lists-data-tables.md) | List/table conventions, syntax types, column typing, and destructive-replace pitfalls. |
| [05 · Curated Rules](05-curated-rules.md) | Google-managed content: rule-set deployment state vs. the full catalog; why curated rules can only be toggled, not edited. |
| [06 · Dashboards](06-dashboards.md) | Native dashboards: listing-vs-full-export, curated vs. custom, and why JSON exports are not hand-edited. |
| [07 · UDM Queries](07-udm-queries.md) | UDM search patterns — and the key lesson: verify `vendor_name`/`log_type` in *your own* data before trusting any curated rule's vendor filter. |
| [08 · Feeds & Parsers](08-feeds-parsers.md) | Ingest health (`state: FAILED` first), secrets never in the repo, and prebuilt vs. custom parsers (grep, don't open). |
| [09 · SOAR Operations](09-soar-operations.md) | Two SOAR API surfaces, the playbook-versioning gotcha, and case hygiene as detection-as-code on cron. |
| [10 · LLM & Automation](10-llm-and-automation.md) | How an LLM agent drives the CLI (deterministic flags, `--json`, dry-run-first) and detection-as-code automation patterns. |
