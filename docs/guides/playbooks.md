# Playbooks: discover, author, operate

Everything playbook-shaped, in the order you actually use it: discover the
building blocks, author (offline or with the API), then operate what runs.
Mutations are guarded throughout (`--dry-run` default, `--yes` to apply); SOAR
auth is the AppKey — see [configure](configure.md).

## 🔎 Discover the palette

The designer's Step Selection panel as read-only catalogs — what a playbook
*can* be built from:

```bash
secopsctl soar playbook components actions              # every action, every integration
secopsctl soar playbook components actions --grep close # find by name/description
secopsctl soar playbook components flow                 # transformers + logical operators
secopsctl soar playbook components triggers             # the trigger vocabulary (offline)
secopsctl soar playbook components blocks               # reusable nested playbooks
secopsctl soar playbook components integrations         # the installed packs
```

Each action row carries its **numeric definition id** — feed it (or just the
name) to the reverse index before touching anything:

```bash
secopsctl soar playbook components usage --action "Close Case" --integration Siemplify
```

— every playbook whose steps reference that action: the impact analysis for
editing or deleting it.

## 📝 Author

**The config-as-code path** (recommended): playbooks are a reconcile surface —
`soar pull playbooks` → edit the JSON → `git diff` → `soar push playbooks`
(`--prune` reports server-only playbooks; delete them with `soar playbook
delete`). One playbook imperatively:
`soar push playbook --file <playbook.json>` (whole-body save; mints a new
version). See [the loop](the-loop.md) and [reconcile](reconcile.md).

**Offline scaffolding**: `soar build-playbook` assembles a playbook from an
exported base + step molds (`soar playbook mold extract/apply`), and
`soar playbook trigger set` edits triggers reviewably — no API calls until
you push. `soar playbook validate` sanity-checks any definition JSON.

**Custom Python definitions** (the IDE's create flow as an API loop):

```bash
secopsctl soar integration action template --integration HTTP        # the skeleton
secopsctl soar integration action create --integration HTTP \
  --name my-action --script ./my_action.py --yes                     # create (guarded)
secopsctl soar integration action delete --integration HTTP --id 42 --yes
```

`job-def` mirrors the same three verbs for scheduled-job definitions. New
definitions land with the template's enabled state (disabled for a fresh
template) — review in the IDE before enabling. Check
`components usage` before any delete.

**AI drafting**: `soar playbook generate --description "<what it should do>"`
returns a generated draft definition *without persisting it* — review, then
save through the normal guarded loop. Instances can restrict the Playbook
Assistant to interactive auth; the verb reports that plainly.

## ▶️ Operate

| Task | Command |
|---|---|
| What exists / is enabled | `soar playbook list [--type nested] [--enabled-only]` |
| Turn one on/off | `soar playbook deploy (--name \| --identifier) --enable\|--disable` |
| Run / rerun on a case | `soar playbook run` · `rerun` · `rerun-block` (explicit case/alert selectors) |
| Debug from an export | `soar playbook debug` + `soar case simulation list/get` |
| A run went wrong | `soar playbook summary` — faulted steps, errors, per-step Logs Explorer links |
| Pending approvals | `soar playbook pending list` → `step execute` (continue) / `step skip` (reject) |
| Run statistics | `soar playbook stats --name <p> --hours 168` |
| Version history / rollback | `soar playbook versions` → `restore --version <id>` (guarded) |
| Promote across tenants | `soar playbook export [--zip]` → `import --file <bundle.zip>` |
| Delete (single or batch) | `soar playbook delete (--name \| --identifier a,b,c \| --from-file <list>)` |

Action results land on the case (`soar playbook results` / `result`); Python
execution logs are `soar playbook python-logs` (can 500 on some instances —
`summary` is the reliable triage path).
