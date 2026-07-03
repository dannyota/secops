# Stats / Aggregation Queries

`secopsctl search stats` runs a YARA-L 2.0 aggregation query against the UDM
search engine (the same `dashboardQueries:execute` path that dashboard charts
use). `search udm` auto-routes queries containing `match:` or `outcome:` to
this engine.

## Sections

A stats query is built from a filter predicate followed by optional named
sections. Order matters — sections must appear in this sequence:

| Section | Purpose | Required |
|---------|---------|----------|
| *(filter)* | Bare predicate lines before the first section header | Yes |
| `match:` | Group-by variables and time-granularity buckets | No |
| `outcome:` | Computed columns via aggregate functions | No |
| `dedup:` | Deduplicate rows by a variable | No |
| `order:` | Sort results (`$var asc` or `$var desc`) | No |
| `limit:` | Cap the number of rows returned | No |

At least one of `match:` or `outcome:` is typically present — without either,
the query is a plain event search (use `search udm` instead).

## Filter

The filter is the bare UDM predicate — the same syntax `search udm` accepts.
It appears before any section header:

```yaral
metadata.event_type = "USER_LOGIN"
principal.hostname != ""
```

## match: (group-by)

Group results by one or more variables. Each variable is prefixed with `$`:

```yaral
match:
  $log_type = metadata.log_type
```

### Time-granularity grouping

Bucket results by time using the `by` or `over every` keywords:

```yaral
match:
  $log_type = metadata.log_type by 2h
```

```yaral
match:
  $log_type = metadata.log_type over every day
```

Supported granularities: `MINUTE`, `HOUR`, `DAY`, `WEEK`, `MONTH`.

The optional `first` keyword returns only the first occurrence per group:

```yaral
match:
  $log_type = metadata.log_type by 2h first
```

Duration literals: `2h`, `30m`, `1d` — or use `over every <granularity>` for
calendar-aligned periods.

## outcome: (aggregates)

Define computed columns using aggregate functions:

```yaral
outcome:
  $event_count = count(metadata.id)
  $unique_users = count_distinct(principal.user.userid)
```

### Aggregate functions

| Function | Description |
|----------|-------------|
| `array(expr)` | Collect values into an array |
| `array_distinct(expr)` | Collect unique values into an array |
| `avg(expr)` | Arithmetic mean |
| `count(expr)` | Count of values |
| `count_distinct(expr)` | Count of unique values |
| `earliest(expr)` | Earliest (minimum) value |
| `latest(expr)` | Latest (maximum) value |
| `max(expr)` | Maximum numeric value |
| `min(expr)` | Minimum numeric value |
| `stddev(expr)` | Standard deviation |
| `sum(expr)` | Sum of numeric values |

## dedup:

Deduplicate rows by a variable:

```yaral
dedup:
  $hostname
```

## order:

Sort the result set:

```yaral
order:
  $event_count desc
```

Multiple sort keys are comma-separated. Each key accepts `asc` (default) or
`desc`.

## limit:

Cap the number of rows:

```yaral
limit: 1000
```

## Search-vs-rules differences

Aggregation queries in UDM search differ from YARA-L 2.0 detection rules:

- **No `over` event windows.** The `over` keyword for multi-event correlation
  windows (e.g. `over 5m`) is a rules-only construct. Time-granularity grouping
  (`by 2h`, `over every day`) is supported.
- **No `condition:` section.** Threshold conditions are rules-only.
- **No `options:` section.** Rule options (allow_zero_values, etc.) do not apply.

## Server limits

- **90-day maximum lookback.** The search window cannot exceed 90 days.
- **10 000 rows maximum.** The server caps results at 10 000 rows per query.

## Examples

### Event count by log type

```yaral
metadata.log_type != ""

match:
  $log_type = metadata.log_type

outcome:
  $count = count(metadata.id)

order:
  $count desc

limit: 50
```

```bash
secopsctl search stats --hours 24 'metadata.log_type != ""
match: $log_type = metadata.log_type
outcome: $count = count(metadata.id)
order: $count desc
limit: 50'
```

### Hourly login trend

Time-bucketed grouping: `by 1h` buckets the group-by variable by event time.

```yaral
metadata.event_type = "USER_LOGIN"

match:
  $app = target.application by 1h

outcome:
  $logins = count(metadata.id)
  $users = count_distinct(principal.user.userid)
```

```bash
secopsctl search stats --hours 72 'metadata.event_type = "USER_LOGIN"
match: $app = target.application by 1h
outcome: $logins = count(metadata.id)
  $users = count_distinct(principal.user.userid)'
```

### Top talkers by bytes

```yaral
metadata.event_type = "NETWORK_CONNECTION"
target.ip != ""

match:
  $dst = target.ip

outcome:
  $bytes = sum(network.sent_bytes)
  $conns = count(metadata.id)

order:
  $bytes desc

limit: 25
```

### Distinct event types per source

```yaral
principal.hostname != ""

match:
  $host = principal.hostname

outcome:
  $types = array_distinct(metadata.event_type)
  $count = count(metadata.id)

order:
  $count desc

limit: 20
```
