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
			"the exact YARA-L a dashboard chart uses. `query udm` runs an event search and\n" +
			"rejects an aggregation with a 400; this executes it over the same\n" +
			"`dashboardQueries:execute` path dashboard charts use and prints the computed\n" +
			"columns and rows (`--json` for the raw result), so a chart query can be\n" +
			"validated end to end from the CLI before `dashboards add-chart`.",
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

// statsCell renders a stats cell (raw JSON) compactly: a JSON string is unquoted,
// everything else is its compact JSON form; an absent cell is "-".
func statsCell(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}
