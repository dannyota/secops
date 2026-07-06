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

// warnReservedChartVars warns (on stderr) when a chart query declares a `$variable`
// whose name collides with a reserved YARA-L keyword — these compile at author time
// but 400 at execute time ("no viable alternative"), rendering a blank chart. Caught
// here at author time so the chart isn't shipped silently broken; rename the variable
// (e.g. `$rule` → `$rule_name`). A clean or empty query is a no-op.
func warnReservedChartVars(query string) {
	if strings.TrimSpace(query) == "" {
		return
	}
	if bad := reservedQueryVars(query); len(bad) > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: chart query uses reserved YARA-L keyword(s) as variable name(s): %s — "+
				"these compile now but 400 at execute time (blank chart); rename them (e.g. $%s → $%s_v)\n",
			strings.Join(bad, ", "), bad[0], bad[0])
	}
}

// tileTypeToken maps the friendly --tile-type flag to the API enum token.
func tileTypeToken(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "visualization":
		return chronicle.TileTypeVisualization, nil
	case "button":
		return chronicle.TileTypeButton, nil
	case "markdown":
		return chronicle.TileTypeMarkdown, nil
	default:
		return "", fmt.Errorf("invalid --tile-type %q (want visualization | button | markdown)", s)
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
	var dryRun, yes, ifAbsent, noFilters bool
	cmd := &cobra.Command{
		Use:   "add <dashboard-id> --title <t> (--query <yaral> | --query-file <f>)",
		Short: "Add a chart with a YARA-L query to a dashboard (guarded)",
		Long: "Add a chart to a native dashboard via :addChart, authoring its YARA-L query\n" +
			"inline (the dashboard body itself is reference-only, so `push dashboards`\n" +
			"cannot do this). The query comes from --query or --query-file.\n\n" +
			"Chart type: pass --chart-type area|bar|gauge|line|map|metrics|pie|scatter|table\n" +
			"with --x/--y (encode\n" +
			"variables) and optional --series-by to GENERATE the visualization instead of\n" +
			"hand-authoring --visualization; --x/--y/--series-by are validated against the\n" +
			"query's declared match:/outcome: variables, so a typo fails clean (not a blank\n" +
			"chart). --if-absent skips the add when a chart with the same title already\n" +
			"exists (idempotent re-runs). Guarded: dry-run by default, --yes to apply.\n" +
			"Re-pull afterwards so local mirrors live.\n\n" +
			"RESERVED WORDS: avoid these as $variable names — they compile but 400 at\n" +
			"execute time (blank chart): rule, private, global, meta, strings, condition,\n" +
			"events, match, outcome, options, dedup, order, limit, select, unselect, and,\n" +
			"or, not, all, any, at, contains, startswith, endswith, icontains, istartswith,\n" +
			"iendswith, iequals, matches, in, over, nocase, ascii, wide, fullword, xor,\n" +
			"base64, base64wide, filesize, entrypoint, int8..uint32/be variants.\n" +
			"Rename collisions (e.g. $rule → $rule_v) before adding.",
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
			warnReservedChartVars(q)
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
			chartID := lastSegment(nestedString(resp, "dashboardChart", "name"))

			// Bind the new chart to GlobalTimeFilter via a dashboard
			// definition PATCH — filtersIds is a definition-level property,
			// not accepted in the :addChart request body.
			if !noFilters && chartID != "" {
				if ferr := bindChartFilter(ctx, c, id, chartID); ferr != nil {
					fmt.Fprintf(os.Stderr, "warning: chart added but filter binding failed: %v\n"+
						"  bind manually: dashboards filters set %s --apply-to %s --yes\n", ferr, id, chartID)
				}
			}

			if jsonOut {
				return emitJSON(resp)
			}
			if chartID != "" {
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
	cmd.Flags().StringVar(&chartType, "chart-type", "", "generate the visualization: area | bar | gauge | line | map | metrics | pie | scatter | table")
	cmd.Flags().StringVar(&encodeX, "x", "", "--chart-type: the category/itemName encode variable (a query match/outcome var)")
	cmd.Flags().StringVar(&encodeY, "y", "", "--chart-type: the value encode variable (a query match/outcome var)")
	cmd.Flags().StringVar(&seriesBy, "series-by", "", "--chart-type bar|line: split into stacked series by this query variable")
	cmd.Flags().StringVar(&tileType, "tile-type", "visualization", "tile type: visualization | button")
	cmd.Flags().BoolVar(&noFilters, "no-filters", false, "do not bind the chart to the dashboard's GlobalTimeFilter")
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
// chartView is one dereferenced chart: the dashboard references the chart, the
// chart references the query — this flattens all three into a reviewable record.
type chartView struct {
	ChartID     string          `json:"chartId"`
	Title       string          `json:"title"`
	TileType    string          `json:"tileType"`
	DataSources []string        `json:"dataSources,omitempty"`
	FiltersIds  []string        `json:"filtersIds,omitempty"`
	ChartLayout json.RawMessage `json:"chartLayout,omitempty"`
	QueryID     string          `json:"queryId,omitempty"`
	Query       string          `json:"query,omitempty"`
	Error       string          `json:"error,omitempty"`
}

func newDashboardsChartsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <dashboard-id>",
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
			// Read the dashboard definition once to get per-chart
			// filtersIds and chartLayout (definition-level fields).
			full, err := c.GetDashboard(ctx, id, true)
			if err != nil {
				return err
			}
			var def struct {
				Definition struct {
					Charts []struct {
						DashboardChart string          `json:"dashboardChart"`
						FiltersIds     []string        `json:"filtersIds"`
						ChartLayout    json.RawMessage `json:"chartLayout"`
					} `json:"charts"`
				} `json:"definition"`
			}
			_ = json.Unmarshal(full.Raw, &def)

			refs := make([]string, 0, len(def.Definition.Charts))
			defByID := map[string]struct {
				FiltersIds  []string
				ChartLayout json.RawMessage
			}{}
			for _, dc := range def.Definition.Charts {
				if dc.DashboardChart != "" {
					refs = append(refs, dc.DashboardChart)
					defByID[lastSegment(dc.DashboardChart)] = struct {
						FiltersIds  []string
						ChartLayout json.RawMessage
					}{dc.FiltersIds, dc.ChartLayout}
				}
			}

			views := make([]chartView, 0, len(refs))
			charts := c.ChartsByID(ctx, refs)
			for _, ref := range refs {
				cid := lastSegment(ref)
				v := chartView{ChartID: cid}
				if de, ok := defByID[cid]; ok {
					v.FiltersIds = de.FiltersIds
					v.ChartLayout = de.ChartLayout
				}
				chartRaw, ok := charts[cid]
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

// bindChartFilter reads the dashboard definition, sets filtersIds on the
// targeted chart entry, and PATCHes the definition.charts array. This is the
// only way to bind a filter to a chart — the :addChart endpoint does not
// accept filtersIds in its request body.
func bindChartFilter(ctx context.Context, c *chronicle.Client, dashboardID, chartID string) error {
	d, err := c.GetDashboard(ctx, dashboardID, true)
	if err != nil {
		return err
	}
	var def struct {
		Definition struct {
			Charts []json.RawMessage `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(d.Raw, &def); err != nil {
		return err
	}
	charts, err := applyFilterToCharts(def.Definition.Charts, chartID)
	if err != nil {
		return err
	}
	_, err = c.UpdateDashboard(ctx, dashboardID, chronicle.DashboardUpdate{Charts: charts})
	return err
}

// indentLines prefixes every line of s with prefix.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
