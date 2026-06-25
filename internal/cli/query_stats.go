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
// search and rejects a `match:`/`outcome:` aggregation with a 400; this posts it
// to the stats path of :udmSearch (`chronicle.GetStats`) and prints the computed
// columns/rows — the CLI way to validate the exact YARA-L a dashboard chart uses
// before it is authored.
func newQueryStatsCmd() *cobra.Command {
	var (
		hours        int
		fromTS, toTS string
		limit        int
	)
	cmd := &cobra.Command{
		Use:   "stats <aggregation-query>",
		Short: "Run a stats/aggregation UDM query (match:/outcome:) and print the result table",
		Long: "Run an AGGREGATION UDM query — one carrying a `match:`/`outcome:` projection,\n" +
			"the exact YARA-L a dashboard chart uses. `query udm` runs an event search and\n" +
			"rejects an aggregation with a 400; this posts it to the stats API and prints\n" +
			"the computed columns and rows (`--json` for the raw result), so a chart query\n" +
			"can be validated end to end from the CLI before `dashboards add-chart`.",
		Example: "  secopsctl query stats --hours 24 'metadata.log_type != \"\"\n" +
			"  match: $lt = metadata.log_type\n" +
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
			res, err := c.GetStats(baseContext(), query, start, end, limit, 0)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			if len(res.Columns) == 0 || res.TotalRows == 0 {
				fmt.Println("no rows.")
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
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	f.IntVar(&limit, "limit", 0, "max values returned per field (0 = server default)")
	return markJSON(cmd)
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
