package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newRulesReviewCmd() *cobra.Command {
	var (
		hours, minDets  int
		format, outFile string
	)
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Read-only: promotion report for monitor-mode rules (enabled, not alerting)",
		Long: "Identify rules that are enabled but NOT alerting (monitor mode) and show\n" +
			"their detection activity over the last --hours. Rules with the most\n" +
			"detections appear first — the best promotion candidates. Use --min-detections\n" +
			"to filter out silent monitors.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			if err := checkHours(hours); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			rules, err := c.ListRules(ctx)
			if err != nil {
				return err
			}
			deps, err := c.ListRuleDeployments(ctx)
			if err != nil {
				return err
			}
			depByID := map[string]*chronicle.RuleDeployment{}
			for i := range deps {
				depByID[deps[i].RuleID()] = &deps[i]
			}
			start, end := timeWindow(hours)
			trends, err := c.GetRulesTrends(ctx, nil, start, end, chronicle.BucketSizeDay)
			if err != nil {
				return err
			}
			trendByID := map[string]*chronicle.RuleTrend{}
			for i := range trends {
				trendByID[trends[i].RuleID] = &trends[i]
			}

			rows := buildReviewRows(rules, depByID, trendByID, minDets)

			w, closeFn, err := openOut(outFile)
			if err != nil {
				return err
			}
			defer func() { _ = closeFn() }()
			return renderReview(w, rows, hours, format)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 168, "detection-trend look-back window in hours (default 7d)")
	f.IntVar(&minDets, "min-detections", 0, "only show rules with at least N detections")
	f.StringVar(&format, "format", "", "output: table | json | csv (default table; overrides the global --output/--json)")
	f.StringVar(&outFile, "out", "", "write to a file instead of stdout")
	return markJSON(cmd)
}

type reviewRow struct {
	RuleID        string `json:"rule_id"`
	DisplayName   string `json:"display_name"`
	Detections    int    `json:"detections_in_window"`
	LastDetection string `json:"last_detection,omitempty"`
	RunFrequency  string `json:"run_frequency,omitempty"`
	CompileState  string `json:"compile_state,omitempty"`
}

func buildReviewRows(rules []chronicle.Rule, depByID map[string]*chronicle.RuleDeployment, trendByID map[string]*chronicle.RuleTrend, minDets int) []reviewRow {
	var rows []reviewRow
	for i := range rules {
		r := &rules[i]
		id := r.RuleID()
		d := depByID[id]

		enabled := r.LiveModeEnabled
		alerting := r.AlertingEnabled
		if d != nil {
			enabled = enabled || d.Enabled
			alerting = alerting || d.Alerting
		}
		if !enabled || alerting {
			continue
		}

		row := reviewRow{
			RuleID:       id,
			DisplayName:  r.DisplayName,
			CompileState: r.CompilationState,
		}
		if d != nil {
			row.RunFrequency = d.RunFrequency
		}
		if t := trendByID[id]; t != nil {
			row.Detections = t.TotalDetections()
			row.LastDetection = t.LastDetectionTime
		}
		if row.Detections < minDets {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Detections != rows[j].Detections {
			return rows[i].Detections > rows[j].Detections
		}
		return rows[i].DisplayName < rows[j].DisplayName
	})
	return rows
}

func renderReview(w io.Writer, rows []reviewRow, hours int, format string) error {
	if format = effectiveFormat(format); format == "" {
		format = "table"
	}
	switch format {
	case "json":
		withDets, zeroDets := 0, 0
		for _, r := range rows {
			if r.Detections > 0 {
				withDets++
			} else {
				zeroDets++
			}
		}
		return writeIndentedValue(w, map[string]any{
			"window_hours": hours,
			"candidates":   rows,
			"summary": map[string]int{
				"total_monitor":   len(rows),
				"with_detections": withDets,
				"zero_detections": zeroDets,
			},
		})
	case "csv":
		csvRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			csvRows = append(csvRows, []string{
				r.RuleID, r.DisplayName, strconv.Itoa(r.Detections),
				r.LastDetection, r.RunFrequency, r.CompileState,
			})
		}
		return printCSVTo(w, []string{
			"rule_id", "display_name", "detections_in_window",
			"last_detection", "run_frequency", "compile_state",
		}, csvRows)
	default: // table
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "RULE ID\tDISPLAY NAME\tDETECTIONS\tLAST DETECTION\tRUN FREQUENCY\tCOMPILE STATE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				r.RuleID, truncate(r.DisplayName, 40), r.Detections,
				orDash(shortTS(r.LastDetection)), orDash(r.RunFrequency), orDash(r.CompileState))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		withDets, zeroDets := 0, 0
		for _, r := range rows {
			if r.Detections > 0 {
				withDets++
			} else {
				zeroDets++
			}
		}
		fmt.Fprintf(w, "\n%d monitor-mode rule(s): %d with detections, %d silent (last %dh).\n",
			len(rows), withDets, zeroDets, hours)
		return nil
	}
}
