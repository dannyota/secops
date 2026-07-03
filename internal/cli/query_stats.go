package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newQueryStatsCmd runs an aggregation (stats) UDM query. `query udm` is an event
// search and rejects a `match:`/`outcome:` aggregation with a 400; this executes it
// over the dashboardQueries:execute path (`chronicle.RunStatsQuery`) — the same
// execution dashboard charts use — and prints the computed columns/rows, so the
// exact YARA-L a chart uses can be validated from the CLI before it is authored.
func newQueryStatsCmd() *cobra.Command {
	var (
		hours        int
		fromTS, toTS string
		limit        int
		clearCache   bool
	)
	cmd := &cobra.Command{
		Use:   "stats <aggregation-query>",
		Short: "Run a stats/aggregation UDM query (match:/outcome:) and print the result table",
		Long: "Run an AGGREGATION UDM query — one carrying a `match:`/`outcome:` projection,\n" +
			"the exact YARA-L a dashboard chart uses. `search udm` auto-routes queries\n" +
			"containing `match:` or `outcome:` here, so either verb works.\n\n" +
			"SECTIONS (in order; only a filter predicate is required):\n\n" +
			"  filter      bare predicate lines before the first section header\n" +
			"  match:      group-by variables ($var); time-granularity grouping:\n" +
			"                match: $x by 2h        (bucket by duration)\n" +
			"                match: $x over every day  (calendar periods)\n" +
			"              granularities: MINUTE, HOUR, DAY, WEEK, MONTH\n" +
			"              optional `first` keyword: `match: $x by 2h first`\n" +
			"  outcome:    computed columns with aggregate functions:\n" +
			"                $alias = function(expression)\n" +
			"              functions: array, array_distinct, avg, count,\n" +
			"              count_distinct, earliest, latest, max, min, stddev, sum\n" +
			"  dedup:      deduplicate by variable (dedup: $var)\n" +
			"  order:      sort results (order: $var asc|desc)\n" +
			"  limit:      cap rows returned (limit: 100)\n\n" +
			"SEARCH-VS-RULES DIFFERENCES: aggregation queries in search do NOT\n" +
			"support `over` event windows, `condition:`, or `options:` sections\n" +
			"(those are rules-only constructs).\n\n" +
			"SERVER LIMITS: 90-day maximum lookback; 10 000 rows max per query.\n\n" +
			"See `docs/tips/stats-queries.md` for a full syntax reference with examples.",
		Example: "  secopsctl search stats --hours 24 'metadata.log_type != \"\"\n" +
			"  match: metadata.log_type\n" +
			"  outcome: $c = count(metadata.id)\n" +
			"  order: $c desc'",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("an aggregation query is required")
			}
			if fromTS == "" {
				if err := checkHours(hours); err != nil {
					return err
				}
			}
			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			res, err := c.RunStatsQuery(baseContext(), query, start, end, clearCache)
			if err != nil {
				return err
			}
			if jsonOut {
				// --json emits the full result (--limit is a human-table print cap only,
				// so a script always sees every row the server returned).
				return emitJSON(res)
			}
			if len(res.Columns) == 0 || res.TotalRows == 0 {
				fmt.Println("no rows.")
				printStatsWarnings(res.Warnings)
				return nil
			}
			rows := res.Rows
			truncated := false
			if limit > 0 && len(rows) > limit {
				rows = rows[:limit]
				truncated = true
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, strings.Join(res.Columns, "\t"))
			for _, row := range rows {
				cells := make([]string, len(res.Columns))
				for i, col := range res.Columns {
					cells[i] = statsCell(row[col])
				}
				fmt.Fprintln(tw, strings.Join(cells, "\t"))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if truncated {
				fmt.Printf("\nshowing %d of %d row(s) (--limit).\n", len(rows), res.TotalRows)
			} else {
				fmt.Printf("\n%d row(s).\n", res.TotalRows)
			}
			printStatsWarnings(res.Warnings)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	f.IntVar(&limit, "limit", 0, "max rows to print (0 = all)")
	f.BoolVar(&clearCache, "clear-cache", false, "bypass the query cache (read from the database)")
	return markJSON(cmd)
}

// printStatsWarnings surfaces non-fatal runtime notices (e.g. a server-side
// row-limit truncation) on stderr, so stdout stays clean and a partial result is
// never silently presented as complete.
func printStatsWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

// statsCell renders a stats cell (raw JSON) compactly: a JSON string is
// unquoted, a JSON array is comma-joined, everything else is its compact JSON
// form; an absent cell is "-".
func statsCell(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Render JSON arrays as comma-joined values for human-readable table output.
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		parts := make([]string, len(arr))
		for i, elem := range arr {
			var sv string
			if json.Unmarshal(elem, &sv) == nil {
				parts[i] = sv
			} else {
				parts[i] = strings.TrimSpace(string(elem))
			}
		}
		return strings.Join(parts, ", ")
	}
	return strings.TrimSpace(string(raw))
}

// runStatsFromUDM executes a stats query on behalf of `search udm` when it
// detects an aggregation query (match:/outcome:), reusing the caller's window
// flags so the same invocation works on either verb.
func runStatsFromUDM(query string, hours int, fromTS, toTS string) error {
	if fromTS == "" {
		if err := checkHours(hours); err != nil {
			return err
		}
	}
	start, end, err := resolveWindow(hours, fromTS, toTS)
	if err != nil {
		return err
	}
	c, err := newChronicleClient()
	if err != nil {
		return err
	}
	res, err := c.RunStatsQuery(baseContext(), query, start, end, false)
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(res)
	}
	if len(res.Columns) == 0 || res.TotalRows == 0 {
		fmt.Println("no rows.")
		printStatsWarnings(res.Warnings)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(res.Columns, "\t"))
	for _, row := range res.Rows {
		cells := make([]string, len(res.Columns))
		for i, col := range res.Columns {
			cells[i] = statsCell(row[col])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%d row(s).\n", res.TotalRows)
	printStatsWarnings(res.Warnings)
	return nil
}
