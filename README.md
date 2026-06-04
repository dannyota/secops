# secopsctl

Operate **Google SecOps (Chronicle SIEM)** as code — for *any* tenant.

`secopsctl` is a single-binary Go CLI **and** an importable, unofficial Go SDK
for Google SecOps, built around the **pull → review → push** detection-as-code
loop:

- **pull** the live instance (rules, reference lists, data tables, dashboards,
  curated-rule-set deployment state, feeds, parsers) into plain local files,
- **review** the change as a `git diff`,
- **push** the reviewed change back to the live instance under guard rails.

It is **tenant-neutral**: there are no project numbers, customer IDs, or
hostnames baked into the code. Everything tenant-specific comes from one config
file. The tool is built so that **both humans and LLM agents** can drive it —
deterministic flags, optional `--json` output, clear `--help`, and no
interactive prompts except the push confirmation (skippable with `--yes`).

> **SAFETY:** `pull` is **read-only** against the instance. **Every `push` is a
> production deploy to a live SIEM.** Mutating commands default to `--dry-run`,
> print a `LIVE DEPLOY` banner with a preview, and change nothing until you pass
> `--yes` (or confirm the prompt). Always run the dry-run, read it, then deploy.

## Install

```bash
go install danny.vn/secops/cmd/secopsctl@latest
# or, from a clone:
go build -o secopsctl ./cmd/secopsctl
```

Requires Go ≥ 1.26. A single static binary — no runtime, no SDK install.

### Authentication

Two surfaces, two credentials. `secopsctl` resolves them lazily, so `--help`,
`info`, and config never touch the network.

**SIEM** (`pull` / `push` / `query` / `doctor`) — **Google ADC, no key to fetch:**
```bash
gcloud auth application-default login    # once; cloud-platform scope
```
The OAuth token is minted in-process from ADC (no `gcloud` shell-out, nothing
written to disk). If you can't use `gcloud`, override with
`GOOGLE_APPLICATION_CREDENTIALS` (a service-account key JSON) or
`SECOPS_ACCESS_TOKEN` (a static bearer).

**SOAR** (`soar …`) — **an AppKey, no ADC.** Generate it once in the Chronicle
**SOAR UI → Settings → Advanced → API Keys → Add** (long-lived, admin-scoped),
then store it in your user config (hidden prompt, written `0600`):
```bash
secopsctl config set-soar-key      # writes SECOPS_SOAR_APP_KEY to ~/.secopsctl/.env
```
secopsctl auto-loads `~/.secopsctl/.env` (and `./.env`) on startup, so the key is
picked up by every command — no manual `export`. You can still set
`SECOPS_SOAR_APP_KEY` in the environment instead; an exported value always wins.
Keep keys in your environment or `~/.secopsctl/.env` — **never in the repo**. (The
Chronicle SIEM API uses no API key — it's OAuth/ADC only.)

## Quickstart

```bash
# 1. Create your instance config from the template (placeholders only).
cp config/instance.example.yaml config/instance.yaml
# edit: project_id, project_number, region, customer_id
#       (+ soar_url if you'll use `soar`)

# 2. Verify config + live connectivity (read-only smoke test).
secopsctl doctor

# 3. Pull some live state (read-only; overwrites local files).
secopsctl pull rules
secopsctl pull reference_lists

# 4. Look around with an ad-hoc UDM query.
secopsctl query udm 'metadata.event_type = "USER_LOGIN"' --hours 24 --json

# 5. (SOAR) snapshot connectors — needs soar_url + SECOPS_SOAR_APP_KEY.
secopsctl soar pull connectors
```

`config/instance.yaml` is git-ignored, so your tenant identifiers never get
committed. Discovery order: `--config` flag → `$SECOPSCTL_CONFIG` →
`./config/instance.yaml` → `~/.secopsctl/instance.yaml` →
`~/.config/secopsctl/instance.yaml`.

## Command surface

```
secopsctl [--config PATH] [--json] <command> ...
```

| Command | What it does |
|---|---|
| `info` | Print the configured instance (sanity-check identifiers). |
| `config set-soar-key` / `config path` | Store the SOAR AppKey in `~/.secopsctl/.env` (hidden prompt, `0600`); show user-config paths. |
| `pull <target> [--filter EXPR] [--out DIR]` | Read-only pull of live state into local files. Targets: `rules`, `reference_lists`, `data_tables`, `dashboards`, `curated`, `curated_rules`, `feeds`, `parsers`, `all`. `--filter` applies to `curated_rules`. |
| `push <target> [--dry-run \| --yes]` | **Mutating, live deploy.** `rules-create` (create rules from `*.yaral` with no companion YAML), `rules-disable` (disable locally-tracked enabled rules). Defaults to `--dry-run`. |
| `query udm <filter> [--hours N] [--from TS] [--to TS] [--limit N] [--json]` | Ad-hoc UDM event search. |

Example UDM filters live in [`examples/queries/`](examples/queries/).

## Use as a Go SDK

The `chronicle` package is a standalone, importable client (pure API — no file
I/O):

```go
import (
    "context"

    "danny.vn/secops/auth"
    "danny.vn/secops/chronicle"
)

func main() {
    c, _ := chronicle.NewClient(chronicle.Settings{
        ProjectID:     "your-project-id",
        ProjectNumber: "000000000000",
        Region:        "us",
        CustomerID:    "00000000-0000-0000-0000-000000000000",
    }, auth.OAuth()) // ADC; credentials resolved lazily on first call

    rules, err := c.ListRules(context.Background())
    _ = rules
    _ = err
}
```

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the SDK surface and what's planned.

## Tips

The [`tips/`](tips/) directory is a friendly, tenant-neutral collection of notes
on operating SecOps as code — the craft, not any one tenant's data. Start at
[`tips/README.md`](tips/README.md).

## License

Licensed under the [Apache License 2.0](LICENSE).
