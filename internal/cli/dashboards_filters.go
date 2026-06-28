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
		Short: "Dashboard filter operations: show, set",
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
			"  --time 30 --unit DAY     (last 30 days)\n" +
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
			target := fmt.Sprintf("set global time filter on dashboard %s to %s", id, summary)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would set global time filter on dashboard %s to %s. Re-run with --yes.\n",
					id, summary)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to set filter without confirmation (pass --yes). Aborted.")
				return nil
			}

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

			if _, err := c.UpdateDashboard(ctx, id, chronicle.DashboardUpdate{Filters: newFilters}); err != nil {
				return err
			}
			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Set global time filter on dashboard %s to %s. Re-pull to mirror it locally.\n", id, summary)
			return nil
		},
	}
	cmd.Flags().IntVar(&timeVal, "time", 0, "time range value (e.g. 24)")
	cmd.Flags().StringVar(&timeUnit, "unit", "", "time range unit: HOUR | DAY | WEEK | MONTH")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("time")
	_ = cmd.MarkFlagRequired("unit")
	return markJSON(cmd)
}
