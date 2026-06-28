package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// chartSpec is one chart in an `charts batch --file` batch. chartType (+ x/y/
// series-by) generates the visualization; alternatively visualization is raw.
type chartSpec struct {
	Title         string          `json:"title"`
	Query         string          `json:"query"`
	ChartType     string          `json:"chartType,omitempty"`
	X             string          `json:"x,omitempty"`
	Y             string          `json:"y,omitempty"`
	SeriesBy      string          `json:"seriesBy,omitempty"`
	TileType      string          `json:"tileType,omitempty"`
	Layout        json.RawMessage `json:"layout,omitempty"`
	Interval      json.RawMessage `json:"interval,omitempty"`
	Datasource    json.RawMessage `json:"datasource,omitempty"`
	Visualization json.RawMessage `json:"visualization,omitempty"`
}

// prepareChartInput validates one spec and builds the AddChart input with the same
// defaults as `charts add` (full-width layout, default interval/datasource).
func prepareChartInput(s chartSpec) (chronicle.AddChartInput, error) {
	if s.Title == "" {
		return chronicle.AddChartInput{}, fmt.Errorf("a chart spec has no title")
	}
	tile, err := tileTypeToken(s.TileType)
	if err != nil {
		return chronicle.AddChartInput{}, fmt.Errorf("chart %q: %w", s.Title, err)
	}
	// A visualization tile needs a query (parity with single add-chart) — else the
	// server creates a permanently blank, queryless chart.
	if s.Query == "" && tile == chronicle.TileTypeVisualization {
		return chronicle.AddChartInput{}, fmt.Errorf("chart %q: a visualization chart needs a query", s.Title)
	}
	vis := s.Visualization
	if s.ChartType != "" {
		if s.Visualization != nil {
			return chronicle.AddChartInput{}, fmt.Errorf("chart %q: set chartType OR visualization, not both", s.Title)
		}
		if err := validateEncodeVars(s.Query, s.X, s.Y, s.SeriesBy); err != nil {
			return chronicle.AddChartInput{}, fmt.Errorf("chart %q: %w", s.Title, err)
		}
		if vis, err = buildVisualization(s.ChartType, s.X, s.Y, s.SeriesBy); err != nil {
			return chronicle.AddChartInput{}, fmt.Errorf("chart %q: %w", s.Title, err)
		}
	}
	layout := s.Layout
	if len(layout) == 0 {
		layout = json.RawMessage(`{"startX":0,"spanX":96,"startY":0,"spanY":16}`)
	}
	interval := s.Interval
	if s.Query != "" && len(interval) == 0 {
		interval = json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`)
	}
	ds := s.Datasource
	if len(ds) == 0 {
		ds = json.RawMessage(`{"dataSources":["UDM"]}`)
	}
	return chronicle.AddChartInput{
		DisplayName:     s.Title,
		TileType:        tile,
		ChartLayout:     layout,
		ChartDatasource: ds,
		Visualization:   vis,
		Query:           s.Query,
		Interval:        interval,
	}, nil
}

func newDashboardsAddChartsCmd() *cobra.Command {
	var file string
	var pace time.Duration
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "batch <dashboard-id> --file <charts.json>",
		Short: "Batch-add charts from a file, paced under quota and idempotent (guarded)",
		Long: "Author a whole dashboard's charts from a JSON array file in one guarded run.\n" +
			"Each spec is {title, query, chartType, x, y, seriesBy, tileType, layout,\n" +
			"interval, datasource, visualization} (same fields as `charts add`). The build\n" +
			"is:\n" +
			"  - validated UP FRONT (every spec's chart-type encode vars vs its query), so a\n" +
			"    bad spec fails before any chart is written;\n" +
			"  - idempotent — a chart whose title already exists on the dashboard is SKIPPED,\n" +
			"    so a re-run after a partial failure converges instead of duplicating;\n" +
			"  - paced (--pace between adds, default 1s) to stay under the per-minute chart\n" +
			"    quota (the transport also retries a 429).\n" +
			"Guarded: dry-run by default, --yes to apply. Re-pull afterwards.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if file == "" {
				return fmt.Errorf("--file is required (a JSON array of chart specs)")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var specs []chartSpec
			if err := json.Unmarshal(raw, &specs); err != nil {
				return fmt.Errorf("parse %s: %w", file, err)
			}
			if len(specs) == 0 {
				return fmt.Errorf("%s contains no chart specs", file)
			}
			// Validate + prepare every spec before any write.
			inputs := make([]chronicle.AddChartInput, len(specs))
			for i, s := range specs {
				if inputs[i], err = prepareChartInput(s); err != nil {
					return err
				}
			}

			target := fmt.Sprintf("add %d chart(s) to dashboard %s", len(specs), id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would add %d chart(s) to dashboard %s (existing titles skipped):\n", len(specs), id)
				for _, s := range specs {
					typ := s.ChartType
					if typ == "" {
						typ = "custom"
					}
					fmt.Printf("  • %s (%s)\n", s.Title, typ)
				}
				fmt.Println("Re-run with --yes.")
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to add charts without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			existing, err := dashboardChartTitles(ctx, c, id)
			if err != nil {
				return err
			}

			added, skipped, failed := 0, 0, 0
			human := !jsonOut
			for _, in := range inputs {
				if _, ok := existing[in.DisplayName]; ok {
					skipped++
					if human {
						fmt.Printf("  skip   %s (already exists)\n", in.DisplayName)
					}
					continue
				}
				if added > 0 && pace > 0 {
					time.Sleep(pace) // stay under the per-minute chart quota
				}
				if _, aerr := c.AddChart(ctx, id, in); aerr != nil {
					failed++
					if human {
						fmt.Printf("  FAIL   %s: %v\n", in.DisplayName, aerr)
					}
					continue
				}
				added++
				existing[in.DisplayName] = "" // dedup within this run too
				if human {
					fmt.Printf("  added  %s\n", in.DisplayName)
				}
			}
			if jsonOut {
				if err := emitJSON(struct {
					Dashboard string `json:"dashboard"`
					Added     int    `json:"added"`
					Skipped   int    `json:"skipped"`
					Failed    int    `json:"failed"`
				}{id, added, skipped, failed}); err != nil {
					return err
				}
			} else {
				fmt.Printf("\nDone. %d added, %d skipped, %d failed. Re-pull to mirror locally.\n", added, skipped, failed)
			}
			if failed > 0 {
				return fmt.Errorf("charts batch: %d chart(s) failed (re-run to retry — existing are skipped)", failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "JSON array of chart specs (required)")
	cmd.Flags().DurationVar(&pace, "pace", time.Second, "delay between adds to stay under the chart quota")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}
