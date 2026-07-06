# Recipes — cross-cutting workflows

Copy-pasteable, end-to-end. Replace placeholders; preview before `--yes`.
Each recipe chains commands that live in different topic-specific tips.

## SOC triage — queue to close

```bash
secopsctl cases list --status open --sort priority --json   # the queue, worst first
secopsctl cases aging --limit 20                            # oldest open + SLA status
secopsctl cases workload                                    # per-analyst load
secopsctl cases get <id> --json                             # case + alerts + firing rules
secopsctl cases overview --id <id>                          # entities + enrichment
secopsctl gemini summarize --case-id <id>                   # AI summary + next steps
secopsctl cases run-action --id <id> --action <name> \
    --instance <uuid> --dry-run                             # integration action
secopsctl cases close --id <id> --reason not-malicious \
    --root-cause '<text>' --comment 'false positive' --yes
```

Per-alert triage (close one alert without closing the case): add `--alert <id>`
to `close`, or `cases alert <verb>`. Bulk: `cases assign|tag|stage --ids 1,2,3`.

## Parser debug pipeline

Pull raw logs for a broken/missing parser (events normalize to `GENERIC_EVENT`)
and pipe straight into a parser test:

```bash
secopsctl search udm \
    'metadata.log_type = "KONG_GATEWAY" AND metadata.event_type = "GENERIC_EVENT"' \
    --raw --limit 50 \
  | secopsctl ingest parsers run KONG_GATEWAY --cbn parser.conf --logs -
```

`--statedump` adds verbose diagnostics per log line.

## User activity audit

Six standard UDM queries for one email (login, admin, password, oauth, iam,
resource), auto-chunking windows over 90 days:

```bash
secopsctl audit user alice@example.com --hours 24
secopsctl audit user alice@example.com --from 2026-01-01 --to 2026-07-01
secopsctl audit user alice@example.com --categories login,admin --format json
secopsctl audit user alice@example.com --format csv          # summary counts
```

## Entity maturity audit

Cross-reference risk scores against watchlist coverage:

```bash
secopsctl entities audit                                     # high-risk vs gaps
secopsctl entities audit --min-risk 200 --json               # lower threshold
secopsctl entities risk-scores --order-by 'riskScore desc'   # rank hosts/users
```

## Ship and tune a detection rule

```bash
secopsctl rules test detections/new-rule.yaral --hours 24    # preview detections
secopsctl rules promote detections/new-rule.yaral --dry-run  # validate + create + deploy
secopsctl rules promote detections/new-rule.yaral --alerting=false --yes
secopsctl rules trends --hours 168                           # noisiest rules
secopsctl rules health --only silent                         # never-firing rules
secopsctl rules review --min-detections 5                    # monitor-mode candidates
```

## Reconcile any config-as-code surface

```bash
secopsctl soar pull connectors
git diff
secopsctl soar push connectors --prune --dry-run
secopsctl soar push connectors --yes
secopsctl soar pull connectors                               # re-pull to mirror
```

The same loop applies to every reconcile surface: `reference_lists`,
`data_tables`, `feeds`, `parsers`, `dashboards`, `forwarders`,
`rule_exclusions`, `soar/webhooks`, `soar/playbooks`, `soar/connectors`,
`soar/jobs`.

## Content Hub install

```bash
secopsctl content-hub browse                                 # totals
secopsctl content-hub list --installed                       # what's installed
secopsctl content-hub diff --identifier <id>                 # local vs. available
secopsctl content-hub install --identifier <id> --dry-run    # preview
secopsctl content-hub install --identifier <id> --yes        # apply
```

## Dashboard authoring (end-to-end)

Validate the query first with `search stats`, then add the chart:

```bash
secopsctl search stats --hours 24 'metadata.log_type != ""
match: metadata.log_type
outcome: $c = count(metadata.id)
order: $c desc'

secopsctl dashboards create --name "SOC Overview" --access public --yes
secopsctl dashboards charts add <id> --title "Log types" \
    --query '<validated query>' --chart-type bar \
    --x metadata.log_type --y '$c' \
    --layout '{"startX":0,"spanX":48,"startY":0,"spanY":16}' --yes
secopsctl dashboards filters set <id> --time 7 --unit DAY --apply-to all --yes
secopsctl dashboards layout show <id>
```

## Scheduled job lifecycle

```bash
secopsctl integrations job-def create --integration <key> \
    --name "My Job" --script job.py --yes
secopsctl soar jobs instance create --integration <key> --job "My Job" \
    --display-name "My Job" --interval 300 --param 'Key=value' --enable --yes
secopsctl soar jobs instance run --instance "My Job" --yes
secopsctl soar jobs instance history --instance "My Job"
secopsctl soar jobs revision create --integration <key> \
    --job <id> --comment "pre-change" --yes
```

Full recipe: [13-scheduled-jobs.md](13-scheduled-jobs.md).
