package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Native-dashboard chart authoring. A dashboard's definition.charts[] only holds
// a resource-name REFERENCE to each chart; the YARA-L query lives one hop further
// in a separate dashboardQuery resource. So a chart and its query are authored
// through the dedicated chart ops (:addChart / :editChart), not by writing the
// dashboard body — which `push dashboards` (reference-only) cannot do. These verbs
// surface those ops; `charts` dereferences a dashboard's charts back to their
// queries for review.

// readChartQuery resolves the YARA-L query from --query (inline) or --query-file.
func readChartQuery(query, queryFile string) (string, error) {
	if queryFile != "" {
		b, err := os.ReadFile(queryFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return query, nil
}

// tileTypeToken maps the friendly --tile-type flag to the API enum token.
func tileTypeToken(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "visualization":
		return chronicle.TileTypeVisualization, nil
	case "button":
		return chronicle.TileTypeButton, nil
	default:
		return "", fmt.Errorf("invalid --tile-type %q (want visualization | button)", s)
	}
}

// rawJSONOrNil returns nil for an empty string, else validates s is JSON and
// returns it as a RawMessage. The field name is for the error message.
func rawJSONOrNil(field, s string) (json.RawMessage, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("--%s is not valid JSON: %s", field, s)
	}
	return json.RawMessage(s), nil
}

// nestedString reads a nested string field from raw JSON, "" if any hop is
// missing or not a string.
func nestedString(raw json.RawMessage, keys ...string) string {
	cur := raw
	for i, k := range keys {
		var m map[string]json.RawMessage
		if json.Unmarshal(cur, &m) != nil {
			return ""
		}
		v, ok := m[k]
		if !ok {
			return ""
		}
		if i == len(keys)-1 {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return ""
			}
			return s
		}
		cur = v
	}
	return ""
}

func newDashboardsAddChartCmd() *cobra.Command {
	var title, query, queryFile, interval, datasource, layout, visualization, tileType string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "add-chart <dashboard-id> --title <t> (--query <yaral> | --query-file <f>)",
		Short: "Add a chart with a YARA-L query to a dashboard (guarded)",
		Long: "Add a chart to a native dashboard via :addChart, authoring its YARA-L query\n" +
			"inline (the dashboard body itself is reference-only, so `push dashboards`\n" +
			"cannot do this). The query comes from --query or --query-file; layout,\n" +
			"datasource, interval, and tile-type have sensible defaults. Guarded: dry-run\n" +
			"by default, --yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			tile, err := tileTypeToken(tileType)
			if err != nil {
				return err
			}
			q, err := readChartQuery(query, queryFile)
			if err != nil {
				return err
			}
			if q == "" && tile == chronicle.TileTypeVisualization {
				return fmt.Errorf("a visualization chart needs a query (pass --query or --query-file)")
			}
			ds, err := rawJSONOrNil("datasource", datasource)
			if err != nil {
				return err
			}
			vis, err := rawJSONOrNil("visualization", visualization)
			if err != nil {
				return err
			}
			lay, err := rawJSONOrNil("layout", layout)
			if err != nil {
				return err
			}
			var iv json.RawMessage
			if q != "" {
				if iv, err = rawJSONOrNil("interval", interval); err != nil {
					return err
				}
				if iv == nil {
					// The server attaches a query only when both query and interval
					// are present; an empty --interval would silently drop the query.
					return fmt.Errorf("a chart query needs a non-empty --interval")
				}
			}

			target := fmt.Sprintf("add chart %q to dashboard %s", title, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would add chart %q (tile=%s) to dashboard %s with query:\n%s\nRe-run with --yes.\n",
					title, tile, id, q)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to add a chart without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			resp, err := c.AddChart(baseContext(), id, chronicle.AddChartInput{
				DisplayName:     title,
				TileType:        tile,
				ChartLayout:     lay,
				ChartDatasource: ds,
				Visualization:   vis,
				Query:           q,
				Interval:        iv,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if chartID := lastSegment(nestedString(resp, "dashboardChart", "name")); chartID != "" {
				fmt.Printf("Added chart %q (id %s) to dashboard %s. Re-pull to mirror it locally.\n", title, chartID, id)
			} else {
				fmt.Printf("Added chart %q to dashboard %s. Re-pull to mirror it locally.\n", title, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "chart display name (required)")
	cmd.Flags().StringVar(&query, "query", "", "inline YARA-L query for the chart")
	cmd.Flags().StringVar(&queryFile, "query-file", "", "read the YARA-L query from a file")
	cmd.Flags().StringVar(&interval, "interval", `{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`, "query input interval (JSON)")
	cmd.Flags().StringVar(&datasource, "datasource", `{"dataSources":["UDM"]}`, "chart datasource (JSON)")
	cmd.Flags().StringVar(&layout, "layout", `{"startX":0,"spanX":12,"startY":0,"spanY":8}`, "chart layout (JSON)")
	cmd.Flags().StringVar(&visualization, "visualization", "", "optional visualization config (JSON)")
	cmd.Flags().StringVar(&tileType, "tile-type", "visualization", "tile type: visualization | button")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("query", "query-file")
	_ = cmd.MarkFlagRequired("title")
	return markJSON(cmd)
}

func newDashboardsEditChartCmd() *cobra.Command {
	var chartID, query, queryFile, interval string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit-chart <dashboard-id> --chart-id <id> (--query <yaral> | --query-file <f>)",
		Short: "Replace a chart's YARA-L query (guarded)",
		Long: "Edit the YARA-L query of an existing chart via :editChart. Resolves the\n" +
			"chart's underlying dashboardQuery and round-trips its etag for optimistic\n" +
			"concurrency. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			q, err := readChartQuery(query, queryFile)
			if err != nil {
				return err
			}
			if q == "" {
				return fmt.Errorf("a new query is required (pass --query or --query-file)")
			}
			iv, err := rawJSONOrNil("interval", interval)
			if err != nil {
				return err
			}

			target := fmt.Sprintf("edit query of chart %s in dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would replace the query of chart %s in dashboard %s with:\n%s\nRe-run with --yes.\n", chartID, id, q)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to edit a chart query without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			chart, err := c.GetChart(ctx, chartID)
			if err != nil {
				return err
			}
			queryRef := nestedString(chart, "chartDatasource", "dashboardQuery")
			if queryRef == "" {
				return fmt.Errorf("chart %s has no query to edit", chartID)
			}
			qres, err := c.GetQuery(ctx, queryRef)
			if err != nil {
				return err
			}
			body := map[string]any{"name": queryRef, "query": q, "etag": nestedString(qres, "etag")}
			if iv != nil {
				body["input"] = iv
			}
			bb, err := json.Marshal(body)
			if err != nil {
				return err
			}
			resp, err := c.EditChart(ctx, id, chronicle.EditChartInput{DashboardQuery: bb})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			fmt.Printf("Edited the query of chart %s in dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id (or resource name) of the chart to edit (required)")
	cmd.Flags().StringVar(&query, "query", "", "new inline YARA-L query")
	cmd.Flags().StringVar(&queryFile, "query-file", "", "read the new YARA-L query from a file")
	cmd.Flags().StringVar(&interval, "interval", "", "optional new query input interval (JSON)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("query", "query-file")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}

func newDashboardsRemoveChartCmd() *cobra.Command {
	var chartID string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "remove-chart <dashboard-id> --chart-id <id>",
		Short: "Remove a chart from a dashboard (guarded)",
		Long: "Remove a chart from a dashboard via :removeChart. Guarded: dry-run by default,\n" +
			"--yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("remove chart %s from dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would remove chart %s from dashboard %s. Re-run with --yes.\n", chartID, id)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to remove a chart without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if _, err := c.RemoveChart(baseContext(), id, chartID); err != nil {
				return err
			}
			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Removed chart %s from dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id (or resource name) of the chart to remove (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}

// chartView is one dereferenced chart: the dashboard references the chart, the
// chart references the query — this flattens all three into a reviewable record.
type chartView struct {
	ChartID     string   `json:"chartId"`
	Title       string   `json:"title"`
	TileType    string   `json:"tileType"`
	DataSources []string `json:"dataSources,omitempty"`
	QueryID     string   `json:"queryId,omitempty"`
	Query       string   `json:"query,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func newDashboardsChartsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "charts <dashboard-id>",
		Short: "List a dashboard's charts with their resolved YARA-L queries (read-only)",
		Long: "Dereference a dashboard's charts back to their queries: the dashboard body\n" +
			"only references each chart by name, so this fetches each chart and its\n" +
			"dashboardQuery and prints the title, tile type, datasources, and YARA-L.\n" +
			"Read-only. Use --json for the full machine-readable list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			full, err := c.GetDashboard(ctx, id, true)
			if err != nil {
				return err
			}
			var def struct {
				Definition struct {
					Charts []struct {
						DashboardChart string `json:"dashboardChart"`
					} `json:"charts"`
				} `json:"definition"`
			}
			if err := json.Unmarshal(full.Raw, &def); err != nil {
				return err
			}

			views := make([]chartView, 0, len(def.Definition.Charts))
			for _, cc := range def.Definition.Charts {
				if cc.DashboardChart == "" {
					continue
				}
				v := chartView{ChartID: lastSegment(cc.DashboardChart)}
				chartRaw, gerr := c.GetChart(ctx, cc.DashboardChart)
				if gerr != nil {
					v.Error = gerr.Error()
					views = append(views, v)
					continue
				}
				var ch struct {
					DisplayName     string `json:"displayName"`
					TileType        string `json:"tileType"`
					ChartDatasource struct {
						DashboardQuery string   `json:"dashboardQuery"`
						DataSources    []string `json:"dataSources"`
					} `json:"chartDatasource"`
				}
				_ = json.Unmarshal(chartRaw, &ch)
				v.Title, v.TileType, v.DataSources = ch.DisplayName, ch.TileType, ch.ChartDatasource.DataSources
				if ref := ch.ChartDatasource.DashboardQuery; ref != "" {
					v.QueryID = lastSegment(ref)
					if qraw, qerr := c.GetQuery(ctx, ref); qerr == nil {
						v.Query = nestedString(qraw, "query")
					} else {
						v.Error = qerr.Error()
					}
				}
				views = append(views, v)
			}

			if jsonOut {
				return emitJSON(views)
			}
			if len(views) == 0 {
				fmt.Printf("Dashboard %s has no charts.\n", id)
				return nil
			}
			fmt.Printf("Dashboard %s — %d chart(s):\n", id, len(views))
			for _, v := range views {
				fmt.Printf("\n• [%s] %s (%s)\n", v.ChartID, v.Title, v.TileType)
				if len(v.DataSources) > 0 {
					fmt.Printf("  datasources: %s\n", strings.Join(v.DataSources, ", "))
				}
				if v.Error != "" {
					fmt.Printf("  error: %s\n", v.Error)
				}
				if v.Query != "" {
					fmt.Printf("  query:\n%s\n", indentLines(v.Query, "    "))
				}
			}
			return nil
		},
	}
	return markJSON(cmd)
}

// indentLines prefixes every line of s with prefix.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
