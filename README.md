# secopsctl

Operate **Google SecOps (Chronicle SIEM + Siemplify SOAR)** as code — for *any* tenant.

`secopsctl` is a single-binary Go CLI **and** an importable, unofficial Go SDK
for Google SecOps. Two products, two planes, one CLI:

- **Control plane — config as code.** **pull** live config (SIEM rules, reference
  lists, data tables, dashboards, curated rule sets, feeds, parsers; SOAR
  webhooks, environments, networks, playbooks, and more) into plain local files →
  **review** the `git diff` → **push** the reviewed change back, reconciled by one
  product-neutral engine.
- **Operational plane — triage.** **query** live data and **act** on it (cases,
  alerts) the way an analyst does — guarded, never reconciled from a file.

See **[`docs/README.md`](docs/README.md)** for the model in one screen and
**[`docs/CATALOG.md`](docs/CATALOG.md)** for what's built and how mature it is.

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
**SOAR UI → Settings → Advanced → API Keys → Add** (long-lived, admin-scoped).
`secopsctl config` prompts for it (hidden) and stores it in your config file
(`~/.secopsctl/instance.yaml`, written `0600`). You can also override it at run
time with `SECOPS_SOAR_APP_KEY` — an exported env var always wins over the file.
The config file is git-ignored, so the key is **never committed**. (The Chronicle
SIEM API uses no API key — it's OAuth/ADC only.)

> **Note:** v1 stores the AppKey in plaintext in the `0600`, git-ignored config
> file. Encrypting it at rest bound to the OS user is on the roadmap.

## Quickstart

```bash
# 1. Set up your instance config (prompts for each value; writes
#    ~/.secopsctl/instance.yaml, 0600). `secopsctl init` is an alias.
secopsctl config
#    (or edit a file by hand: cp config/instance.example.yaml config/instance.yaml)

# 2. Verify config + live connectivity (read-only smoke test).
secopsctl doctor

# 3. Pull some live state (read-only; overwrites local files).
secopsctl pull rules
secopsctl pull reference_lists

# 4. Look around with an ad-hoc UDM query.
secopsctl query udm 'metadata.event_type = "USER_LOGIN"' --hours 24 --json

# 5. (SOAR) snapshot connectors — needs soar_url + the SOAR AppKey.
secopsctl soar pull connectors
```

The config file is git-ignored, so your tenant identifiers (and the SOAR AppKey,
if stored there) never get committed. Resolution, highest priority first
(secopsctl does **not** read `.env`): real `SECOPS_*` env vars → the file at
`--config` / `$SECOPSCTL_CONFIG` → `~/.secopsctl/instance.yaml` →
`./config/instance.yaml` → `~/.config/secopsctl/instance.yaml`.

## Command surface

```
secopsctl [--config PATH] [--json] <command> ...
```

| Command | What it does |
|---|---|
| `info` | Print the configured instance (sanity-check identifiers; AppKey redacted). |
| `config` (alias `init`) | Set up the config in `~/.secopsctl/instance.yaml` via a single-screen form (edit all fields, AppKey hidden, Save/Cancel), or takes flags / `--non-interactive`. |
| `doctor` | Read-only live smoke test of config + connectivity. |
| `pull <target> [--filter EXPR] [--out DIR]` | Read-only pull of live SIEM config into local files. Targets: `rules`, `reference_lists`, `data_tables`, `dashboards`, `curated`, `curated_rules`, `feeds`, `parsers`, `all`. `--filter` applies to `curated_rules`. |
| `push <target> [--dry-run \| --yes] [--prune]` | **Mutating, live deploy.** `rules-create` / `rules-disable`, plus engine-reconciled SIEM config surfaces (e.g. `reference_lists`). Defaults to `--dry-run`. |
| `query udm <filter> [--hours N] [--from TS] [--to TS] [--limit N] [--json]` | Ad-hoc UDM event search. |
| `soar pull <surface> [--out DIR]` | Read-only snapshot of SOAR config (webhooks, environments, networks, playbooks, connectors, jobs, …). |
| `soar push <surface> [--prune] [--dry-run \| --yes]` | **Mutating, live deploy.** Reconcile local SOAR config files to live (create/update; `--prune` to delete). |
| `soar case <verb> …` | Per-case triage verbs (assign / rename / stage / tag / close / merge, …). |
| `soar legacy call <op>` | Raw passthrough to the Siemplify external API for batch/bundle endpoints. |

Example UDM filters live in [`examples/queries/`](examples/queries/). The full
command/surface list and its maturity is in [`docs/CATALOG.md`](docs/CATALOG.md).

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

## Documentation

The [`docs/`](docs/) directory is the design + status contract for the project:

| Doc | What it is |
|---|---|
| [`docs/README.md`](docs/README.md) | The map: the model in one screen + index |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | How it works — engine, lanes, planes, auth, reliability, API-version map |
| [`docs/CATALOG.md`](docs/CATALOG.md) | Status matrix (designed / built / validated) for every surface — the tracker |
| [`docs/SOAR-DESIGN.md`](docs/SOAR-DESIGN.md) · [`docs/SIEM-DESIGN.md`](docs/SIEM-DESIGN.md) | Per-product specifics |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | The forward plan — waves toward the finished tool |

## Tips

The [`tips/`](tips/) directory is a friendly, tenant-neutral collection of notes
on operating SecOps as code — the craft, not any one tenant's data. Start at
[`tips/README.md`](tips/README.md).

## License

Licensed under the [Apache License 2.0](LICENSE).
