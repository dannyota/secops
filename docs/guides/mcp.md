# MCP server

Use secopsctl as a [Model Context Protocol](https://modelcontextprotocol.io)
server to give AI agents — Claude Code, Claude Desktop, Cursor, Windsurf, or
any MCP client — direct access to your Google SecOps (Chronicle SIEM +
Siemplify SOAR) tenant.

The server uses **dynamic tool loading**: instead of exposing all 360+ commands
upfront, it starts with 5 meta-tools and loads group-specific typed tools on
demand. This keeps the agent's context window small while covering every
command.

## Install

```bash
go install danny.vn/secops/cmd/secopsctl@latest
```

Or download a pre-built binary from the
[releases page](https://github.com/dannyota/secops/releases).

## Configure credentials

The MCP server reads the same config as the CLI. Run the one-screen wizard:

```bash
secopsctl config
```

This writes `~/.secopsctl/instance.yaml` (0600, git-ignored). You need up to
four identifiers — project ID, region, customer ID, and SOAR URL — plus
credentials for each plane:

- **SIEM** — Google ADC (minted in-process, nothing on disk)
- **SOAR** — a long-lived AppKey

SIEM alone gives a clean `doctor` — add SOAR whenever you need it. See
[Configure & auth](configure.md) for the full walkthrough.

Verify connectivity:

```bash
secopsctl doctor
```

## Register with your MCP client

### Claude Code

From your project directory:

```bash
secopsctl mcp install
```

This writes an entry to `.mcp.json` in the current directory. Restart Claude
Code to pick up the new server.

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "secopsctl": {
      "command": "secopsctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Ensure `~/.secopsctl/instance.yaml` exists (from `secopsctl config`), or set
`SECOPS_*` environment variables in an `env` block.

### Cursor / other MCP clients

Point your client at the stdio command:

```text
secopsctl mcp serve
```

Ensure credentials are configured via `~/.secopsctl/instance.yaml` or
environment variables.

## How it works

The server exposes:

- **Meta-tools** — `help`, `run`, `focus`, `unfocus`, `usage` (always loaded)
- **Group tools** — loaded on demand via `focus` (e.g. `focus group="cases"`)
- **Resources** — embedded craft guides (`tips://{name}`)

### Workflow

1. Call `help` to discover available command groups
2. Call `help group="cases"` to list subcommands in a group
3. Call `focus group="cases"` to load typed tools (`cases_list`, `cases_get`, …)
4. Use the typed tools with full parameter schemas
5. Call `unfocus group="cases"` when done to free context
6. Use `run` anytime for quick one-off commands without focusing
7. Use `usage command="search udm"` to preview a schema and auto-load it

### Quick path vs typed path

- **`run`** — escape hatch for one-off commands. No schema validation.
  Use shell-style quoting for values with spaces:
  `--filter 'event.type = "LOGIN"'`.
- **`focus`** — the preferred path. Loads typed tools with validated parameters,
  descriptions, and enum hints. Prefer this for commands with filter
  expressions — typed parameters avoid quoting issues.
- **`usage`** — preview one command's schema and auto-load it as a callable
  tool, without loading the whole group.

### Example conversation

> **You:** How many open cases do we have?
>
> The agent calls `run command="cases list --limit 0 --status OPEN"` and
> returns the count.

> **You:** Show me the latest 5 detection alerts.
>
> The agent calls `focus group="alerts"`, then `alerts_list` with
> `limit=5`.

> **You:** Search for login events in the last 24 hours.
>
> The agent calls `focus group="search"`, then `search_udm` with
> `args="metadata.event_type = \"USER_LOGIN\""` and `hours=24`.

All tool output is JSON. Mutations are dry-run by default — the agent must
pass `yes=true` to apply, same as the CLI.

## Security

- **Two-plane auth.** SIEM uses Google ADC (org-scoped OAuth); SOAR uses an
  AppKey. The MCP server inherits whatever credentials are configured.
- **Dry-run by default.** All mutations require `yes=true` — the agent cannot
  accidentally modify your environment without explicit confirmation.
- **Read-only mode.** Set `SECOPS_READONLY=1` or pass `--read-only` to block
  all mutations at the transport level.
- **Local stdio only.** No network listener is opened.
- **Security flags enforced.** `--read-only`, `--non-interactive`, and
  `--no-progress` are forwarded unconditionally to subprocesses — callers
  cannot override them.
