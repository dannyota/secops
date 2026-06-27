# Playbooks: discover, author, operate

Everything playbook-shaped, in the order you actually use it: discover the
building blocks, author (offline or with the API), then operate what runs.
Mutations are guarded throughout (`--dry-run` default, `--yes` to apply); SOAR
auth is the AppKey — see [configure](configure.md).

## 🔎 Discover the palette

The designer's Step Selection panel as read-only catalogs — what a playbook
*can* be built from:

```bash
secopsctl soar playbooks components actions              # every action, every integration
secopsctl soar playbooks components actions --grep close # find by name/description
secopsctl soar playbooks components flow                 # transformers + logical operators
secopsctl soar playbooks components triggers             # the trigger vocabulary (offline)
secopsctl soar playbooks components blocks               # reusable nested playbooks
secopsctl soar playbooks components integrations         # the installed packs
```

Each action row carries its **numeric definition id** — feed it (or just the
name) to the reverse index before touching anything:

```bash
secopsctl soar playbooks components usage --action "Close Case" --integration Siemplify
```

— every playbook whose steps reference that action: the impact analysis for
editing or deleting it.

## 📝 Author

**The config-as-code path** (recommended): playbooks are a reconcile surface —
`soar pull playbooks` → edit the JSON → `git diff` → `soar push playbooks`
(`--prune` reports server-only playbooks; delete them with `soar playbooks
delete`). One playbook imperatively:
`soar push playbook --file <playbook.json>` (whole-body save; mints a new
version). See [the loop](the-loop.md) and [reconcile](reconcile.md).

**Offline scaffolding**: `soar ide build-playbook` assembles a playbook from an
exported base + step molds (`soar playbooks mold extract/apply`),
`soar playbooks step insert` splices a brand-new action step into the graph
(fresh identity, rewired relations — `--after` an anchor, `--branch` for a
condition branch), and `soar playbooks trigger set` edits triggers reviewably —
no API calls until you push. `soar playbooks validate` sanity-checks any
definition JSON.

**Custom Python definitions** (the IDE's create flow as an API loop):

```bash
secopsctl soar integrations action template --integration HTTP        # the skeleton
secopsctl soar integrations action create --integration HTTP \
  --name my-action --script ./my_action.py --yes                     # create (guarded)
secopsctl soar integrations action update --integration HTTP \
  --file ./edited_action.json --yes                                  # update (guarded)
secopsctl soar integrations action delete --integration HTTP --id 42 --yes
```

`job-def` mirrors the same four verbs for scheduled-job definitions. New
definitions land with the template's enabled state (disabled for a fresh
template) — review in the IDE before enabling. Check
`components usage` before any delete.

**AI drafting**: `soar playbooks generate --description "<what it should do>"`
returns a generated draft definition *without persisting it* — review, then
save through the normal guarded loop. Instances can restrict the Playbook
Assistant to interactive auth; the verb reports that plainly.

## ▶️ Operate

| Task | Command |
|---|---|
| What exists / is enabled | `soar playbooks list [--type nested] [--enabled-only]` |
| Turn one on/off | `soar playbooks deploy (--name \| --identifier) --enable\|--disable` |
| Run / rerun on a case | `soar playbooks run` · `rerun` · `rerun-block` (explicit case/alert selectors) |
| Debug from an export | `soar playbooks debug` + `cases simulation list/get` |
| A run went wrong | `soar playbooks summary` — faulted steps, errors, per-step Logs Explorer links |
| Pending approvals | `soar playbooks pending list` → `step execute` (continue) / `step skip` (reject) |
| Run statistics | `soar playbooks stats --name <p> --hours 168` |
| Version history / rollback | `soar playbooks versions` → `restore --version <id>` (guarded) |
| Promote across tenants | `soar playbooks export [--zip]` → `import --file <bundle.zip>` |
| Delete (single or batch) | `soar playbooks delete (--name \| --identifier a,b,c \| --from-file <list>)` |

Action results land on the case (`soar playbooks results` / `result`); Python
execution logs are `soar playbooks python-logs` (can 500 on some instances —
`summary` is the reliable triage path).
