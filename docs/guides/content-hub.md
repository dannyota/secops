# Content Hub

Browse the SecOps **Content Hub** — the marketplace of integrations and content
packs — and install or uninstall them as code. Read verbs are free; `install`
and `uninstall` are **guarded mutations** (dry-run by default, `--yes` to apply).

> 🌐 **Content Hub lives on the SOAR plane** (`<tenant>.siemplify-soar.com`,
> AppKey auth) — not the Chronicle host, where the same surface 500s. So
> `content-hub` needs `soar_url` and a SOAR AppKey configured, exactly like the
> `soar` group. See [configure](configure.md). The two-host rule is in the
> [SOAR design](../design/soar.md).

## Commands

| Command | Kind | Does |
|---|---|---|
| `content-hub browse` | read | overview: integration + content-pack totals and installed counts |
| `content-hub list [--installed]` | read | list marketplace integrations (`--installed` filters to installed) |
| `content-hub get <identifier>` | read | show one marketplace integration |
| `content-hub contentpacks` | read | list content packs |
| `content-hub contentpacks get <identifier>` | read | show one content pack |
| `content-hub install --identifier <id>` | guarded | install a marketplace integration |
| `content-hub uninstall --identifier <id>` | guarded | uninstall an installed integration |

Two more read/guarded verbs round it out: `content-hub diff <integration-id>`
(diff the installed vs. marketplace version of an integration) and
`content-hub featured list` / `featured install` (Google-curated featured
playbooks).

## Browse and inspect

```bash
secopsctl content-hub browse                 # totals + installed counts at a glance
secopsctl content-hub list                   # all marketplace integrations
secopsctl content-hub list --installed       # only what's already installed
secopsctl content-hub get <identifier>       # one integration's detail
```

The `identifier` is the marketplace integration id printed by
`content-hub list`. Add `--json` to any read verb for the machine-readable
record. Content packs work the same way:

```bash
secopsctl content-hub contentpacks           # list packs
secopsctl content-hub contentpacks get <identifier>
```

## Install and uninstall

Both write verbs require `--identifier <id>` and follow the standard guarded
flow: **dry-run by default**, `--yes` to apply.

```bash
secopsctl content-hub install --identifier <id>          # preview (dry-run)
secopsctl content-hub install --identifier <id> --yes    # apply (LIVE DEPLOY)

secopsctl content-hub uninstall --identifier <id>        # preview
secopsctl content-hub uninstall --identifier <id> --yes  # apply
```

Run the preview first, read what it would change, then re-run with `--yes`. A
hard read-only session (`--read-only` / `SECOPS_READONLY=1`) degrades both to a
preview even with `--yes`.

## Recipe: browse, install, configure

Install an integration from the Content Hub, then turn it into a working,
configured instance. Installing puts the integration's actions and connectors
into your environment; **configuring** binds an instance with credentials and
settings (via the `soar integrations` group).

```bash
# 1. Find the integration you want and note its identifier.
secopsctl content-hub list --json | grep -i '<name>'

# 2. Preview, then install it (guarded).
secopsctl content-hub install --identifier <id>          # dry-run preview
secopsctl content-hub install --identifier <id> --yes    # apply

# 3. Confirm it landed.
secopsctl content-hub list --installed
secopsctl soar integrations instances --integration <id>

# 4. Configure an instance of it (credentials + settings) — guarded.
#    --param is repeatable; use env:VAR for secrets. Dry-run first, then --yes.
secopsctl soar integrations configure --integration <id> --param key=value
```

`soar integrations configure` is the post-install step that supplies the
integration's parameters; see [Playbooks](playbooks.md) and the
[Catalog](../design/catalog.md) for the full integration lifecycle
(install → configure → instances → connectors → jobs).

## See also

- [Configure](configure.md) — set `soar_url` + the AppKey the Content Hub needs.
- [Playbooks](playbooks.md) — build and operate playbooks on the installed packs.
- [SOAR design](../design/soar.md) — the two-host rule and SOAR-plane surfaces.
- [Catalog](../design/catalog.md) — per-surface status.
