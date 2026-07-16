package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Dashboard filter commands: show and set the global time filter on a
// dashboard. The dashboard's definition.filters[] array holds the global
// time range (and any advanced token filters).

// dashboardFilter is a parsed filter entry for display.
type dashboardFilter struct {
	ID          string   `json:"id"`
	IsTimeRange bool     `json:"isTimeRange"`
	Values      []string `json:"values,omitempty"`
	Summary     string   `json:"summary"`
}

// parseDashboardFilters extracts the filters from a dashboard's raw JSON.
func parseDashboardFilters(raw json.RawMessage) []dashboardFilter {
	var def struct {
		Definition struct {
			Filters []struct {
				ID                           string `json:"id"`
				IsStandardTimeRangeFilter    bool   `json:"isStandardTimeRangeFilter"`
				FilterOperatorAndFieldValues []struct {
					FieldValues []string `json:"fieldValues"`
				} `json:"filterOperatorAndFieldValues"`
			} `json:"filters"`
		} `json:"definition"`
	}
	if json.Unmarshal(raw, &def) != nil {
		return nil
	}
	out := make([]dashboardFilter, 0, len(def.Definition.Filters))
	for _, f := range def.Definition.Filters {
		df := dashboardFilter{
			ID:          f.ID,
			IsTimeRange: f.IsStandardTimeRangeFilter || f.ID == "GlobalTimeFilter",
		}
		for _, op := range f.FilterOperatorAndFieldValues {
			df.Values = append(df.Values, op.FieldValues...)
		}
		if df.IsTimeRange && len(df.Values) >= 2 {
			df.Summary = df.Values[0] + " " + df.Values[1]
		} else if len(df.Values) > 0 {
			df.Summary = strings.Join(df.Values, ", ")
		}
		out = append(out, df)
	}
	return out
}

func newDashboardsFiltersGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filters <verb>",
		Short: "Manage dashboard filters (show, set global time range)",
	}
	cmd.AddCommand(
		newDashboardsFiltersShowCmd(),
		newDashboardsFiltersSetCmd(),
	)
	return cmd
}

func newDashboardsFiltersShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <dashboard-id>",
		Short: "Show a dashboard's filters (global time range, advanced) — read-only",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			d, err := c.GetDashboard(baseContext(), args[0], true)
			if err != nil {
				return err
			}
			filters := parseDashboardFilters(d.Raw)
			if jsonOut {
				return emitJSON(filters)
			}
			if len(filters) == 0 {
				fmt.Printf("Dashboard %s has no filters.\n", args[0])
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTYPE\tVALUE")
			for _, f := range filters {
				typ := "advanced"
				if f.IsTimeRange {
					typ = "time-range"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", f.ID, typ, f.Summary)
			}
			return tw.Flush()
		},
	}
	return markJSON(cmd)
}

func newDashboardsFiltersSetCmd() *cobra.Command {
	var timeVal int
	var timeUnit string
	var applyTo string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "set <dashboard-id> --time <N> --unit <UNIT>",
		Short: "Set the global time range filter on a dashboard (guarded)",
		Long: "Set or replace the dashboard's global time range filter. The filter\n" +
			"controls the default time window for all charts. Common values:\n" +
			"  --time 1 --unit HOUR     (last hour)\n" +
			"  --time 24 --unit HOUR    (last 24 hours)\n" +
			"  --time 7 --unit DAY      (last 7 days)\n" +
			"  --time 14 --unit DAY     (last 14 days)\n" +
			"  --time 30 --unit DAY     (last 30 days)\n\n" +
			"Use --apply-to to bind the filter to charts in the same PATCH:\n" +
			"  --apply-to all           bind GlobalTimeFilter to every chart\n" +
			"  --apply-to id1,id2       bind to specific chart IDs (bare or full ref)\n" +
			"Without --apply-to, only definition.filters is updated (chart bindings unchanged).\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			unit := strings.ToUpper(timeUnit)
			switch unit {
			case "HOUR", "DAY", "WEEK", "MONTH":
			default:
				return fmt.Errorf("invalid --unit %q (want HOUR | DAY | WEEK | MONTH)", timeUnit)
			}
			if timeVal <= 0 {
				return fmt.Errorf("--time must be a positive integer")
			}

			summary := fmt.Sprintf("%d %s", timeVal, unit)
			action := fmt.Sprintf("set global time filter on dashboard %s to %s", id, summary)
			if applyTo != "" {
				action += fmt.Sprintf(" (apply-to: %s)", applyTo)
			}
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				ctx := baseContext()

				d, err := c.GetDashboard(ctx, id, true)
				if err != nil {
					return err
				}
				var def struct {
					Definition struct {
						Filters []json.RawMessage `json:"filters"`
						Charts  []json.RawMessage `json:"charts"`
					} `json:"definition"`
				}
				_ = json.Unmarshal(d.Raw, &def)

				timeFilter, _ := json.Marshal(map[string]any{
					"id":                        "GlobalTimeFilter",
					"isStandardTimeRangeFilter": true,
					"filterOperatorAndFieldValues": []map[string]any{
						{"fieldValues": []string{fmt.Sprintf("%d", timeVal), unit}},
					},
				})

				newFilters := []json.RawMessage{}
				for _, f := range def.Definition.Filters {
					fid := nestedString(f, "id")
					if fid == "GlobalTimeFilter" {
						continue
					}
					var isTR struct {
						IsStandardTimeRangeFilter bool `json:"isStandardTimeRangeFilter"`
					}
					if json.Unmarshal(f, &isTR) == nil && isTR.IsStandardTimeRangeFilter {
						continue
					}
					newFilters = append(newFilters, f)
				}
				newFilters = append([]json.RawMessage{timeFilter}, newFilters...)

				upd := chronicle.DashboardUpdate{Filters: newFilters}

				// --apply-to: bind GlobalTimeFilter to targeted charts in the same PATCH.
				if applyTo != "" {
					charts, err := applyFilterToCharts(def.Definition.Charts, applyTo)
					if err != nil {
						return err
					}
					upd.Charts = charts
				}

				if _, err := c.UpdateDashboard(ctx, id, upd); err != nil {
					return err
				}
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&timeVal, "time", 0, "time range value (e.g. 24)")
	cmd.Flags().StringVar(&timeUnit, "unit", "", "time range unit: HOUR | DAY | WEEK | MONTH")
	cmd.Flags().StringVar(&applyTo, "apply-to", "", `bind the filter to charts: "all" or comma-separated chart IDs`)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("time")
	_ = cmd.MarkFlagRequired("unit")
	return markJSON(cmd)
}

// applyFilterToCharts sets filtersIds=["GlobalTimeFilter"] on the targeted
// chart entries. target is "all" or a comma-separated list of chart IDs.
func applyFilterToCharts(charts []json.RawMessage, target string) ([]json.RawMessage, error) {
	filterIDs, _ := json.Marshal([]string{"GlobalTimeFilter"})
	isAll := strings.EqualFold(target, "all")

	var wanted map[string]bool
	if !isAll {
		wanted = make(map[string]bool)
		for id := range strings.SplitSeq(target, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				wanted[lastSegment(id)] = true
			}
		}
		if len(wanted) == 0 {
			return nil, fmt.Errorf("--apply-to: no chart IDs provided")
		}
	}

	matched := 0
	out := make([]json.RawMessage, len(charts))
	for i, raw := range charts {
		ref := nestedString(raw, "dashboardChart")
		seg := lastSegment(ref)
		if isAll || wanted[seg] {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			m["filtersIds"] = filterIDs
			nb, err := json.Marshal(m)
			if err != nil {
				return nil, err
			}
			out[i] = nb
			matched++
		} else {
			out[i] = raw
		}
	}
	if !isAll && matched == 0 {
		return nil, fmt.Errorf("--apply-to: none of the specified chart IDs found on the dashboard")
	}
	return out, nil
}
