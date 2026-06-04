# 06 · Native Dashboards

Chronicle's **native dashboards** (the visualizations built on UDM/YARA-L data
in the SecOps UI) are tracked in the repo as JSON. This doc covers the one thing
that trips people up — *listing* a dashboard is not the same as *exporting* it —
plus the curated-vs-custom distinction and why you don't hand-edit the JSON.

For the general pull/review/push loop see
[01-secops-as-code.md](01-secops-as-code.md); for the client mechanics see
[02-architecture-client.md](02-architecture-client.md).

## Listing vs. full export

There are two very different operations, and `pull dashboards` does the cheap
one by design:

- **Listing entry** — what `pull dashboards` fetches: the inventory record for
  each dashboard (its server `name`, display name, type, owner, access). This is
  enough to answer "what dashboards exist and who owns them," and it is fast and
  light. It is **not** the chart definitions.
- **Full export** — the complete dashboard definition: every chart, its UDM/YARA-L
  query, layout, and filters. This is a separate, heavier call (the SDK's
  `export_dashboard([name])`), done per dashboard when you actually need the
  contents.

The split exists because a full export of every dashboard is large and slow, and
most of the time you only want the inventory. Pull the listing for an overview;
export the specific dashboards whose internals you need.

> **Gotcha:** for a **curated** dashboard the listing entry's *definition is
> empty* — there is nothing to diff from the listing alone. If you see an empty
> definition, that is expected for curated content; use the full export (and even
> then, see below).

## Curated vs. custom

Dashboards come in two flavors, mirroring the rule story in
[05-curated-rules.md](05-curated-rules.md):

| Type | Origin | What you can do |
|---|---|---|
| **Curated** | Google-managed, shipped with the product | View; the listing carries no editable definition. Treat as read-only reference. |
| **Custom** (private / shared) | Built in your tenant | Carries a real definition you can export, version, and (carefully) rebuild. |

Custom dashboards are the ones worth tracking closely — they encode your team's
operational views. Curated dashboards are inventory-only from the tool's
perspective.

## Don't hand-edit the JSON

A dashboard export is large and noisy — nested chart specs, query strings,
layout coordinates, style blocks. **Don't hand-edit it unless you understand the
schema.** A malformed edit can break the dashboard on import or silently drop a
chart. Prefer building/adjusting in the UI and re-pulling, or making narrow,
well-understood programmatic edits. Either way, review the resulting `git diff`
before any push — the JSON diff is the audit trail.

## Naming AI-built artifacts

If an automated agent *creates* a dashboard (or any other entity) in the live
instance, give it a distinguishing display-name prefix — e.g. a bracketed tag
like `[Automation]` — so humans can tell agent-built content from
human-built at a glance. (Pick a convention and keep it consistent; this is
operational hygiene, not a tool requirement.) Renaming a custom dashboard is done
by updating its display name via the SDK. Remember from
[02-architecture-client.md](02-architecture-client.md): a display-name change
moves the slug, so treat a rename as an intentional act.
