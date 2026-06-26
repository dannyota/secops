package cli

import (
	"context"
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
	var chartType, encodeX, encodeY, seriesBy string
	var dryRun, yes, ifAbsent bool
	cmd := &cobra.Command{
		Use:   "add-chart <dashboard-id> --title <t> (--query <yaral> | --query-file <f>)",
		Short: "Add a chart with a YARA-L query to a dashboard (guarded)",
		Long: "Add a chart to a native dashboard via :addChart, authoring its YARA-L query\n" +
			"inline (the dashboard body itself is reference-only, so `push dashboards`\n" +
			"cannot do this). The query comes from --query or --query-file.\n\n" +
			"Chart type: pass --chart-type bar|line|pie|table with --x/--y (encode\n" +
			"variables) and optional --series-by to GENERATE the visualization instead of\n" +
			"hand-authoring --visualization; --x/--y/--series-by are validated against the\n" +
			"query's declared match:/outcome: variables, so a typo fails clean (not a blank\n" +
			"chart). --if-absent skips the add when a chart with the same title already\n" +
			"exists (idempotent re-runs). Guarded: dry-run by default, --yes to apply.\n" +
			"Re-pull afterwards so local mirrors live.",
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
			// --chart-type generates the visualization (validating the encode
			// variables against the query) instead of a hand-written --visualization.
			// A table needs no encode mapping, so it skips the variable validation.
			if chartType != "" {
				if !chartTypeIsTable(chartType) {
					if err := validateEncodeVars(q, encodeX, encodeY, seriesBy); err != nil {
						return err
					}
				}
				if vis, err = buildVisualization(chartType, encodeX, encodeY, seriesBy); err != nil {
					return err
				}
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
			ctx := baseContext()
			// --if-absent: skip when a chart with this title already exists, so a
			// re-run after a partial failure converges instead of duplicating. The
			// live read is in the apply path (after the guard), so a --dry-run never
			// makes an API call.
			if ifAbsent {
				titles, terr := dashboardChartTitles(ctx, c, id)
				if terr != nil {
					return terr
				}
				if existing, ok := titles[title]; ok {
					if jsonOut {
						return emitGuardedResult(target, false, false)
					}
					fmt.Printf("chart %q already exists (id %s) on dashboard %s — skipped (--if-absent).\n", title, existing, id)
					return nil
				}
			}
			resp, err := c.AddChart(ctx, id, chronicle.AddChartInput{
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
	cmd.Flags().StringVar(&layout, "layout", `{"startX":0,"spanX":96,"startY":0,"spanY":16}`, "chart layout on the 96-column grid (JSON: startX/spanX 0–96, startY/spanY); default is full-width")
	cmd.Flags().StringVar(&visualization, "visualization", "", "optional raw visualization config (JSON); or use --chart-type to generate it")
	cmd.Flags().StringVar(&chartType, "chart-type", "", "generate the visualization: bar | line | pie | table")
	cmd.Flags().StringVar(&encodeX, "x", "", "--chart-type: the category/itemName encode variable (a query match/outcome var)")
	cmd.Flags().StringVar(&encodeY, "y", "", "--chart-type: the value encode variable (a query match/outcome var)")
	cmd.Flags().StringVar(&seriesBy, "series-by", "", "--chart-type bar|line: split into stacked series by this query variable")
	cmd.Flags().StringVar(&tileType, "tile-type", "visualization", "tile type: visualization | button")
	cmd.Flags().BoolVar(&ifAbsent, "if-absent", false, "skip the add when a chart with the same title already exists (idempotent)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("query", "query-file")
	cmd.MarkFlagsMutuallyExclusive("visualization", "chart-type")
	_ = cmd.MarkFlagRequired("title")
	return markJSON(cmd)
}

// editChartLayout repositions ONE chart by replacing only its `chartLayout` entry
// in the dashboard's definition.charts, then PATCHing the whole (otherwise
// unchanged) charts array via UpdateDashboard. chart_layout is not an :editChart
// field, so this is the supported in-place layout edit. Every other chart's
// reference/layout/filters are preserved verbatim, so no chart is dropped.
func editChartLayout(ctx context.Context, c *chronicle.Client, dashboardID, chartID string, layout json.RawMessage) error {
	full, err := c.GetDashboard(ctx, dashboardID, true)
	if err != nil {
		return err
	}
	var def struct {
		Definition struct {
			Charts []json.RawMessage `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		return err
	}
	want := lastSegment(chartID)
	found := false
	for i, raw := range def.Definition.Charts {
		ref := nestedString(raw, "dashboardChart")
		if ref == "" || lastSegment(ref) != want {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		m["chartLayout"] = layout
		nb, err := json.Marshal(m)
		if err != nil {
			return err
		}
		def.Definition.Charts[i] = nb
		found = true
		break
	}
	if !found {
		return fmt.Errorf("chart %s not found on dashboard %s", chartID, dashboardID)
	}
	_, err = c.UpdateDashboard(ctx, dashboardID, chronicle.DashboardUpdate{Charts: def.Definition.Charts})
	return err
}

// dashboardChartRefs returns the dashboardChart resource-name references in a
// dashboard's definition.charts (the single place the dashboard body lists its
// charts). The one decode shared by `charts`, `verify`, and dedup-by-title.
func dashboardChartRefs(ctx context.Context, c *chronicle.Client, dashboardID string) ([]string, error) {
	full, err := c.GetDashboard(ctx, dashboardID, true)
	if err != nil {
		return nil, err
	}
	var def struct {
		Definition struct {
			Charts []struct {
				DashboardChart string `json:"dashboardChart"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(def.Definition.Charts))
	for _, cc := range def.Definition.Charts {
		if cc.DashboardChart != "" {
			refs = append(refs, cc.DashboardChart)
		}
	}
	return refs, nil
}

// dashboardChartTitles maps a dashboard's existing chart display names to their
// chart ids — used by --if-absent / batch authoring to dedup by title. The
// per-chart GetChart is intentionally serial: a parallel burst over a large
// dashboard would itself risk the per-minute chart-read quota this feature exists
// to avoid.
func dashboardChartTitles(ctx context.Context, c *chronicle.Client, dashboardID string) (map[string]string, error) {
	refs, err := dashboardChartRefs(ctx, c, dashboardID)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	for _, ref := range refs {
		if chartRaw, gerr := c.GetChart(ctx, ref); gerr == nil {
			if t := nestedString(chartRaw, "displayName"); t != "" {
				titles[t] = lastSegment(ref)
			}
		}
	}
	return titles, nil
}

func newDashboardsEditChartCmd() *cobra.Command {
	var chartID, query, queryFile, interval, visualization, layout string
	var chartType, encodeX, encodeY, seriesBy string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit-chart <dashboard-id> --chart-id <id>",
		Short: "Edit a chart's query, visualization, or layout in place (guarded)",
		Long: "Edit an existing chart via :editChart, in place (no remove + re-add that would\n" +
			"churn the chart id / grid position / order). Change any of:\n" +
			"  --query / --query-file   the YARA-L query (etag-guarded dashboardQuery);\n" +
			"  --visualization <json>   the raw visualization, OR --chart-type bar|line|pie|\n" +
			"    table with --x/--y/--series-by to GENERATE it (encode vars validated against\n" +
			"    the chart's query);\n" +
			"  --layout <json>          the chart's grid position (applied via the\n" +
			"    dashboard's definition.charts — chart_layout is not an :editChart field —\n" +
			"    preserving every other chart).\n" +
			"At least one is required. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			q, err := readChartQuery(query, queryFile)
			if err != nil {
				return err
			}
			iv, err := rawJSONOrNil("interval", interval)
			if err != nil {
				return err
			}
			visRaw, err := rawJSONOrNil("visualization", visualization)
			if err != nil {
				return err
			}
			layRaw, err := rawJSONOrNil("layout", layout)
			if err != nil {
				return err
			}
			changingViz := visualization != "" || chartType != ""
			if q == "" && !changingViz && layout == "" {
				return fmt.Errorf("nothing to edit: pass --query/--query-file, --visualization or --chart-type, and/or --layout")
			}

			target := fmt.Sprintf("edit chart %s in dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would edit chart %s in dashboard %s (query=%v viz=%v layout=%v). Re-run with --yes.\n",
					chartID, id, q != "", changingViz, layout != "")
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to edit a chart without confirmation (pass --yes). Aborted.")
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
			in := chronicle.EditChartInput{}

			// Query edit (and the effective query text, for --chart-type validation).
			queryRef := nestedString(chart, "chartDatasource", "dashboardQuery")
			effQuery := q
			if q != "" || iv != nil || chartType != "" {
				if queryRef == "" {
					if q != "" || iv != nil {
						return fmt.Errorf("chart %s has no query to edit", chartID)
					}
				} else {
					qres, qerr := c.GetQuery(ctx, queryRef)
					if qerr != nil {
						return qerr
					}
					if effQuery == "" {
						effQuery = nestedString(qres, "query")
					}
					if q != "" || iv != nil {
						body := map[string]any{"name": queryRef, "query": effQuery, "etag": nestedString(qres, "etag")}
						if iv != nil {
							body["input"] = iv
						}
						if in.DashboardQuery, err = json.Marshal(body); err != nil {
							return err
						}
					}
				}
			}

			// --chart-type generates the visualization, validated against effQuery
			// (a table carries no encode mapping, so it skips var validation).
			if chartType != "" {
				if !chartTypeIsTable(chartType) {
					if err := validateEncodeVars(effQuery, encodeX, encodeY, seriesBy); err != nil {
						return err
					}
				}
				if visRaw, err = buildVisualization(chartType, encodeX, encodeY, seriesBy); err != nil {
					return err
				}
			}

			// Visualization edit via :editChart. A table conversion has no
			// visualization, so it is sent as an explicit null to CLEAR the existing
			// one (an omitted key would leave the old visualization in place).
			if changingViz {
				cbody := map[string]any{"name": nestedString(chart, "name"), "etag": nestedString(chart, "etag")}
				if visRaw != nil {
					cbody["visualization"] = visRaw
				} else {
					cbody["visualization"] = nil
				}
				if in.DashboardChart, err = json.Marshal(cbody); err != nil {
					return err
				}
			}

			// chart_layout is NOT an :editChart field — it lives in the dashboard's
			// definition.charts — so a layout change goes through a definition.charts
			// PATCH that preserves every other chart (see editChartLayout).
			if len(in.DashboardQuery) > 0 || len(in.DashboardChart) > 0 {
				resp, eerr := c.EditChart(ctx, id, in)
				if eerr != nil {
					return eerr
				}
				if jsonOut && layRaw == nil {
					return emitJSON(resp)
				}
			}
			if layRaw != nil {
				if err := editChartLayout(ctx, c, id, chartID, layRaw); err != nil {
					return err
				}
			}
			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Edited chart %s in dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id (or resource name) of the chart to edit (required)")
	cmd.Flags().StringVar(&query, "query", "", "new inline YARA-L query")
	cmd.Flags().StringVar(&queryFile, "query-file", "", "read the new YARA-L query from a file")
	cmd.Flags().StringVar(&interval, "interval", "", "optional new query input interval (JSON)")
	cmd.Flags().StringVar(&visualization, "visualization", "", "new raw visualization config (JSON); or use --chart-type")
	cmd.Flags().StringVar(&chartType, "chart-type", "", "generate the visualization: bar | line | pie | table")
	cmd.Flags().StringVar(&encodeX, "x", "", "--chart-type: the category/itemName encode variable")
	cmd.Flags().StringVar(&encodeY, "y", "", "--chart-type: the value encode variable")
	cmd.Flags().StringVar(&seriesBy, "series-by", "", "--chart-type bar|line: split into stacked series by this query variable")
	cmd.Flags().StringVar(&layout, "layout", "", "new chart layout on the 96-column grid (JSON)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("query", "query-file")
	cmd.MarkFlagsMutuallyExclusive("visualization", "chart-type")
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
			refs, err := dashboardChartRefs(ctx, c, id)
			if err != nil {
				return err
			}

			views := make([]chartView, 0, len(refs))
			charts := c.ChartsByID(ctx, refs) // one batchGet for a healthy dashboard
			for _, ref := range refs {
				v := chartView{ChartID: lastSegment(ref)}
				chartRaw, ok := charts[lastSegment(ref)]
				if !ok {
					v.Error = "chart not found (dangling reference)"
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
