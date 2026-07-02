package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Health status buckets, worst-first.
const (
	healthFailing  = "failing"  // does not compile
	healthErroring = "erroring" // deployment execution limited/paused
	healthSilent   = "silent"   // enabled + alerting but no detections in the window
	healthHealthy  = "healthy"
)

func healthRank(s string) int {
	switch s {
	case healthFailing:
		return 0
	case healthErroring:
		return 1
	case healthSilent:
		return 2
	default:
		return 3
	}
}

// newRulesHealthCmd rolls every rule's health into one worst-first table — the
// console's Detections Health Dashboard as data. It composes existing reads
// (rule list + deployments + detection trends), no new endpoint. Read-only.
func newRulesHealthCmd() *cobra.Command {
	var (
		hours       int
		only        string
		format, out string
	)
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Read-only: per-rule health roll-up (compile / execution / silence) across all rules",
		Long: "One read that classifies every rule by health and lists the worst first:\n" +
			"  failing  — does not compile\n" +
			"  erroring — deployment execution is limited/paused\n" +
			"  silent   — enabled + alerting but produced no detections in the window\n" +
			"  healthy  — everything else\n" +
			"Composes the rule list, deployments, and detection trends over the last\n" +
			"--hours. --only narrows to one bucket.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			if only != "" && healthRank(only) == 3 && only != healthHealthy {
				return fmt.Errorf("--only must be failing, erroring, silent, or healthy (got %q)", only)
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

			rows := buildHealthRows(rules, depByID, trendByID)
			if only != "" {
				kept := rows[:0]
				for _, r := range rows {
					if r.Status == only {
						kept = append(kept, r)
					}
				}
				rows = kept
			}

			w, closeFn, err := openOut(out)
			if err != nil {
				return err
			}
			defer func() { _ = closeFn() }()
			return renderHealth(w, rows, hours, format)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 168, "detection-trend look-back window in hours (default 7d)")
	f.StringVar(&only, "only", "", "show only one bucket: failing | erroring | silent | healthy")
	f.StringVar(&format, "format", "", "output: table | json | csv (default table; overrides the global --output/--json)")
	f.StringVar(&out, "out", "", "write to a file instead of stdout")
	return markJSON(cmd)
}

type healthRow struct {
	RuleID        string `json:"rule_id"`
	DisplayName   string `json:"display_name"`
	Compile       string `json:"compile_state,omitempty"`
	Execution     string `json:"execution_state,omitempty"`
	Enabled       bool   `json:"enabled"`
	Alerting      bool   `json:"alerting"`
	Detections    int    `json:"detections_in_window"`
	LastDetection string `json:"last_detection,omitempty"`
	Status        string `json:"status"`
}

func buildHealthRows(rules []chronicle.Rule, depByID map[string]*chronicle.RuleDeployment, trendByID map[string]*chronicle.RuleTrend) []healthRow {
	rows := make([]healthRow, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		id := r.RuleID()
		row := healthRow{
			RuleID:      id,
			DisplayName: r.DisplayName,
			Compile:     r.CompilationState,
			Enabled:     r.LiveModeEnabled,
			Alerting:    r.AlertingEnabled,
		}
		if d := depByID[id]; d != nil {
			row.Execution = d.ExecutionState
			// Deployment is authoritative for enabled/alerting if the rule view omitted them.
			row.Enabled = row.Enabled || d.Enabled
			row.Alerting = row.Alerting || d.Alerting
		}
		if t := trendByID[id]; t != nil {
			row.Detections = t.TotalDetections()
			row.LastDetection = t.LastDetectionTime
		}
		row.Status = classifyHealth(row)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if ri, rj := healthRank(rows[i].Status), healthRank(rows[j].Status); ri != rj {
			return ri < rj
		}
		return rows[i].DisplayName < rows[j].DisplayName
	})
	return rows
}

func classifyHealth(r healthRow) string {
	if r.Compile != "" && !strings.Contains(strings.ToUpper(r.Compile), "SUCC") {
		return healthFailing
	}
	switch strings.ToUpper(r.Execution) {
	case "LIMITED", "PAUSED", "STOPPED":
		return healthErroring
	}
	if r.Enabled && r.Alerting && r.Detections == 0 {
		return healthSilent
	}
	return healthHealthy
}

func renderHealth(w io.Writer, rows []healthRow, hours int, format string) error {
	if format = effectiveFormat(format); format == "" {
		format = "table"
	}
	switch format {
	case "json":
		return writeIndentedValue(w, map[string]any{"summary": healthSummary(rows), "window_hours": hours, "rules": rows})
	case "csv":
		csvRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			csvRows = append(csvRows, []string{
				r.RuleID, r.DisplayName, r.Compile, r.Execution,
				strconv.FormatBool(r.Enabled), strconv.FormatBool(r.Alerting),
				strconv.Itoa(r.Detections), r.LastDetection, r.Status,
			})
		}
		return printCSVTo(w, []string{
			"rule_id", "display_name", "compile_state", "execution_state",
			"enabled", "alerting", "detections_in_window", "last_detection", "status",
		}, csvRows)
	default: // table
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "STATUS\tRULE ID\tDISPLAY NAME\tCOMPILE\tEXEC\tEN\tAL\tDETS\tLAST DETECTION")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%v\t%v\t%d\t%s\n",
				r.Status, r.RuleID, truncate(r.DisplayName, 40), orDash(r.Compile), orDash(r.Execution),
				r.Enabled, r.Alerting, r.Detections, orDash(shortTS(r.LastDetection)))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		s := healthSummary(rows)
		fmt.Fprintf(w, "\n%d rule(s): %d failing, %d erroring, %d silent, %d healthy (last %dh).\n",
			len(rows), s[healthFailing], s[healthErroring], s[healthSilent], s[healthHealthy], hours)
		return nil
	}
}

func healthSummary(rows []healthRow) map[string]int {
	s := map[string]int{healthFailing: 0, healthErroring: 0, healthSilent: 0, healthHealthy: 0}
	for _, r := range rows {
		s[r.Status]++
	}
	return s
}
