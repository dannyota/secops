# 04 · Reference Lists & Data Tables

Reference lists and data tables are the lookup structures your YARA-L rules
([03-yara-l-rules.md](03-yara-l-rules.md)) and UDM queries
([07-udm-queries.md](07-udm-queries.md)) read against — allowlists, known-IP
ranges, asset inventories, enrichment tables. Both are tracked as code and follow
the same slug-file convention as everything else
([02-architecture-client.md](02-architecture-client.md)).

## Reference lists

A reference list is an ordered set of single-value entries you reference from a
rule (e.g. `$ip in %trusted_ranges`). Each list is two files:

```
reference_lists/<slug>.txt    # one entry per line — the source of truth
reference_lists/<slug>.yaml   # metadata
```

The `.txt` is authoritative: **one entry per line, no commas, no quotes** — a
plain list, not CSV. The companion YAML records the metadata:

```yaml
display_name: Trusted Source Ranges
name:         projects/.../referenceLists/...
description:  Internal egress CIDR ranges
syntax_type:  REFERENCE_LIST_SYNTAX_TYPE_CIDR
scope_info:   ...
entry_count:  12
```

### `syntax_type` must match the entries

The syntax type tells Chronicle how to interpret each line, and the entries must
conform or the list is invalid:

| `syntax_type` | Entry shape | Used for |
|---|---|---|
| `..._PLAIN_TEXT_STRING` | literal strings, one per line | exact-match allow/deny of names, IDs, hashes |
| `..._CIDR` | CIDR blocks (e.g. `10.0.0.0/8`) | IP-range membership tests in rules |
| `..._REGEX` | regular expressions, one per line | pattern matching against fields |

Pick the type for how the rule consumes the list. A CIDR list lets a rule test IP
membership directly; a regex list matches patterns; a plain list does exact
string membership. Mixing shapes (a hostname in a CIDR list) makes the list
unusable.

### Editing a reference list

`pull reflists` writes the `.txt` (one line per entry) and the `.yaml` (with the
server-resolved `syntax_type` and `entry_count`). To change membership, edit the
`.txt`, keep one entry per line, and push through your deploy flow. Pull-before-
edit applies as everywhere — a list someone grew in the UI will be clobbered if
you push stale local lines ([01-secops-as-code.md](01-secops-as-code.md)).

## Data tables

A data table is a multi-column, typed table (think a small reference database a
rule can join against). Each table is two files:

```
data_tables/<slug>.csv    # rows, with a header row from the column names
data_tables/<slug>.yaml   # column types + metadata
```

The CSV holds the rows; the YAML holds the schema:

```yaml
display_name: Asset Owners
name:         projects/.../dataTables/...
description:  Host → owning team mapping
columns:
  - originalColumn: hostname
    columnType: STRING
  - originalColumn: owner
    columnType: STRING
row_count: 240
```

### Column types come from the YAML, not the CSV

Do **not** infer column types from CSV contents — the YAML `columns` block is
authoritative. The CSV header is written from the column names, but the *types*
are fixed in the YAML. This matters because **changing a column type is a
destructive replace on push**: the table is recreated, not patched. Add or rename
columns deliberately and expect a full rebuild of the table when types change.

### Keep row order stable

`pull datatables` writes rows in the server-side order. **Preserve that order**
when you edit so `git diff` shows only the rows you actually changed, not a
spurious whole-table reshuffle. A clean diff is the entire point of reviewing
changes before deploy. (The puller caps rows at a configurable `max_rows` to keep
very large tables manageable — large tables write the first `max_rows` rows.)

## When to use which

- **Reference list** — a single column of values you test membership against
  (`in %list`). Simple, fast, the right tool for allowlists and known-range sets.
- **Data table** — you need multiple correlated columns (a lookup that returns a
  value, e.g. host → owner → environment) and typed columns.

Both are referenced from rule logic; design the lookup structure alongside the
rule that consumes it ([03-yara-l-rules.md](03-yara-l-rules.md)). For lookups
against Google's managed detection content rather than your own, see
[05-curated-rules.md](05-curated-rules.md).
