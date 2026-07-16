package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// editChartLayout replaces a single chart's chartLayout inside the dashboard's
// definition.charts[], then PATCHes the whole (otherwise unchanged) charts array
// via UpdateDashboard. chart_layout is NOT an :editChart field — it lives in the
// dashboard body.
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

// editChartFilters sets the filtersIds for a chart in the dashboard's
// definition.charts[], then PATCHes the whole (otherwise unchanged) charts
// array via UpdateDashboard. Same mechanism as editChartLayout.
func editChartFilters(ctx context.Context, c *chronicle.Client, dashboardID, chartID string, filterIDs []string) error {
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
		b, err := json.Marshal(filterIDs)
		if err != nil {
			return err
		}
		m["filtersIds"] = b
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
	var chartID, title, query, queryFile, interval, visualization, layout string
	var chartType, encodeX, encodeY, seriesBy string
	var filters []string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit <dashboard-id> --chart-id <id>",
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
			warnReservedChartVars(q)
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
			changingTitle := title != ""
			changingFilters := len(filters) > 0
			if q == "" && !changingViz && !changingTitle && !changingFilters && layout == "" {
				return fmt.Errorf("nothing to edit: pass --title, --query/--query-file, --visualization or --chart-type, --filters, and/or --layout")
			}

			target := fmt.Sprintf("edit chart %s in dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would edit chart %s in dashboard %s (title=%v query=%v viz=%v filters=%v layout=%v). Re-run with --yes.\n",
					chartID, id, changingTitle, q != "", changingViz, changingFilters, layout != "")
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

			needsChartEdit := q != "" || iv != nil || changingViz || changingTitle
			var chart json.RawMessage
			if needsChartEdit || layout != "" {
				chart, err = c.GetChart(ctx, chartID)
				if err != nil {
					return err
				}
			}
			in := chronicle.EditChartInput{}

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

			if changingViz || changingTitle {
				cbody := map[string]any{"name": nestedString(chart, "name"), "etag": nestedString(chart, "etag")}
				if changingTitle {
					cbody["displayName"] = title
				}
				if changingViz {
					if visRaw != nil {
						cbody["visualization"] = visRaw
					} else {
						cbody["visualization"] = nil
					}
				}
				if in.DashboardChart, err = json.Marshal(cbody); err != nil {
					return err
				}
			}

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
			if changingFilters {
				if err := editChartFilters(ctx, c, id, chartID, filters); err != nil {
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
	cmd.Flags().StringVar(&title, "title", "", "new chart display name")
	cmd.Flags().StringVar(&query, "query", "", "new inline YARA-L query")
	cmd.Flags().StringVar(&queryFile, "query-file", "", "read the new YARA-L query from a file")
	cmd.Flags().StringVar(&interval, "interval", "", "optional new query input interval (JSON)")
	cmd.Flags().StringVar(&visualization, "visualization", "", "new raw visualization config (JSON); or use --chart-type")
	cmd.Flags().StringVar(&chartType, "chart-type", "", "generate the visualization: area | bar | gauge | line | map | metrics | pie | scatter | table")
	cmd.Flags().StringVar(&encodeX, "x", "", "--chart-type: the category/itemName encode variable")
	cmd.Flags().StringVar(&encodeY, "y", "", "--chart-type: the value encode variable")
	cmd.Flags().StringVar(&seriesBy, "series-by", "", "--chart-type bar|line: split into stacked series by this query variable")
	cmd.Flags().StringVar(&layout, "layout", "", "new chart layout on the 96-column grid (JSON)")
	cmd.Flags().StringSliceVar(&filters, "filters", nil, "set the chart's filter bindings (e.g. GlobalTimeFilter)")
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
		Use:   "remove <dashboard-id> --chart-id <id>",
		Short: "Remove a chart from a dashboard (guarded)",
		Long: "Remove a chart from a dashboard via :removeChart. Guarded: dry-run by default,\n" +
			"--yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("remove chart %s from dashboard %s", chartID, id)
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				if _, err := c.RemoveChart(baseContext(), id, chartID); err != nil {
					return err
				}
				if !jsonOut {
					fmt.Printf("Removed chart %s from dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id (or resource name) of the chart to remove (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}
