package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Dashboard chart EXECUTION — the "verify" half of dashboard authoring. A chart's
// config (`dashboards charts`) says nothing about what it renders; these verbs run
// the chart's query through `dashboardQueries:execute` and surface the computed
// values, so a blank/errored chart is diagnosable from the CLI/CI, not only the UI.

// execQueryRef fetches a stored dashboard query (GetQuery) and executes it,
// returning the freeform result JSON. An empty/absent query is an error.
func execQueryRef(ctx context.Context, c *chronicle.Client, queryRef string, filters []json.RawMessage, clearCache *bool) (json.RawMessage, error) {
	if queryRef == "" {
		return nil, fmt.Errorf("chart has no query to execute")
	}
	qres, err := c.GetQuery(ctx, queryRef)
	if err != nil {
		return nil, err
	}
	var q struct {
		Query string          `json:"query"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(qres, &q); err != nil {
		return nil, err
	}
	if strings.TrimSpace(q.Query) == "" {
		return nil, fmt.Errorf("chart query is empty")
	}
	return c.ExecuteQuery(ctx, q.Query, q.Input, filters, clearCache)
}

// execChartByID dereferences a chart to its stored query (GetChart → GetQuery) and
// executes it — the by-chart-id convenience for `run-chart`.
func execChartByID(ctx context.Context, c *chronicle.Client, chartRef string, filters []json.RawMessage, clearCache *bool) (json.RawMessage, error) {
	chart, err := c.GetChart(ctx, chartRef)
	if err != nil {
		return nil, err
	}
	queryRef := nestedString(chart, "chartDatasource", "dashboardQuery")
	if queryRef == "" {
		return nil, fmt.Errorf("chart %s has no query to execute", lastSegment(chartRef))
	}
	return execQueryRef(ctx, c, queryRef, filters, clearCache)
}

// execResultRowCount counts the data rows in a `dashboardQueries:execute` /
// `:udmSearch` response. That response is COLUMN-major — `results` is an array of
// `{column, values[]}` — so the row count is the longest `values` column, NOT the
// number of columns. known=false when no recognized container is present, so a
// caller must then NOT treat the chart as empty (no false alarm on an unfamiliar
// shape).
func execResultRowCount(raw json.RawMessage) (count int, known bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return 0, false
	}
	arrLen := func(b json.RawMessage) (int, bool) {
		var a []json.RawMessage
		if json.Unmarshal(b, &a) == nil {
			return len(a), true
		}
		return 0, false
	}

	// Primary shape: `results` is a list of columns, each with a `values` array.
	// Rows = the longest column. An empty `results` (no columns) or all-empty
	// `values` is 0 rows. A plain `results: [scalar,…]` (no `values` key) falls
	// through to the generic array handling below.
	if rv, ok := m["results"]; ok {
		var cols []struct {
			Values []json.RawMessage `json:"values"`
		}
		if json.Unmarshal(rv, &cols) == nil && len(cols) > 0 {
			hasValuesKey := false
			rows := 0
			for _, c := range cols {
				if c.Values != nil {
					hasValuesKey = true
				}
				rows = max(rows, len(c.Values))
			}
			if hasValuesKey {
				return rows, true
			}
		}
	}

	// Other shapes: a top-level/`dataTable` row array. Return the first NON-EMPTY
	// recognized container; 0/known only when recognized container(s) are all empty.
	anyKnown := false
	consider := func(b json.RawMessage, ok bool) (int, bool) {
		if !ok {
			return 0, false
		}
		if n, isArr := arrLen(b); isArr {
			anyKnown = true
			if n > 0 {
				return n, true
			}
		}
		return 0, false
	}
	for _, key := range []string{"rows", "results", "series", "values"} {
		if n, hit := consider(m[key], m[key] != nil); hit {
			return n, true
		}
	}
	if dt, ok := m["dataTable"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(dt, &inner) == nil {
			if n, hit := consider(inner["rows"], inner["rows"] != nil); hit {
				return n, true
			}
		}
	}
	if anyKnown {
		return 0, true
	}
	return 0, false
}

func newDashboardsRunChartCmd() *cobra.Command {
	var chartID, filter string
	var clearCache bool
	cmd := &cobra.Command{
		Use:     "run-chart <dashboard-id> --chart-id <id>",
		Aliases: []string{"values"},
		Short:   "Execute a chart's query and print the computed values (read-only)",
		Long: "Execute a chart's stored query via `dashboardQueries:execute` and print the\n" +
			"computed rows/series — the values a chart actually renders (legend labels,\n" +
			"axis categories, series values), which `dashboards charts` (config only)\n" +
			"never shows. Read-only. --json for the raw result, --clear-cache to bypass the\n" +
			"query cache, --filter to apply a JSON dashboard-filter array.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if chartID == "" {
				return fmt.Errorf("--chart-id is required (from `dashboards charts %s`)", id)
			}
			filters, err := parseFilterArg(filter)
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			var cc *bool
			if clearCache {
				cc = &clearCache
			}
			res, err := execChartByID(baseContext(), c, chartID, filters, cc)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, res)
			}
			if n, known := execResultRowCount(res); known {
				fmt.Printf("chart %s: %d row(s).\n", chartID, n)
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id (or resource name) of the chart to execute (required)")
	cmd.Flags().StringVar(&filter, "filter", "", "optional dashboard-filter JSON array applied to the query")
	cmd.Flags().BoolVar(&clearCache, "clear-cache", false, "bypass the query cache (read from the database)")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}

// chartVerdict is one chart's health in `dashboards verify`.
type chartVerdict struct {
	ChartID string `json:"chartId"`
	Title   string `json:"title"`
	Status  string `json:"status"` // ok | empty | error
	Rows    *int   `json:"rows,omitempty"`
	Error   string `json:"error,omitempty"`
}

func newDashboardsVerifyCmd() *cobra.Command {
	var clearCache bool
	cmd := &cobra.Command{
		Use:   "verify <dashboard-id>",
		Short: "Execute every chart and flag the ones returning no rows or an error (read-only)",
		Long: "A dashboard health check: execute each chart's query (`dashboardQueries:execute`)\n" +
			"and report which charts return an ERROR or 0 rows (EMPTY) vs OK — so a blank or\n" +
			"broken chart is caught headless / in CI without opening the UI. Read-only;\n" +
			"exits non-zero (2) when any chart is empty or errored. --json for the full report.",
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
			var cc *bool
			if clearCache {
				cc = &clearCache
			}

			verdicts := make([]chartVerdict, 0, len(refs))
			bad := 0
			for _, ref := range refs {
				v := chartVerdict{ChartID: lastSegment(ref), Status: "ok"}
				// One GetChart per chart: read the title AND the query ref from it,
				// then execute the query directly (no second GetChart).
				chartRaw, gerr := c.GetChart(ctx, ref)
				if gerr != nil {
					v.Status, v.Error, bad = "error", gerr.Error(), bad+1
					verdicts = append(verdicts, v)
					continue
				}
				v.Title = nestedString(chartRaw, "displayName")
				queryRef := nestedString(chartRaw, "chartDatasource", "dashboardQuery")
				res, eerr := execQueryRef(ctx, c, queryRef, nil, cc)
				switch {
				case eerr != nil:
					v.Status, v.Error, bad = "error", eerr.Error(), bad+1
				default:
					if n, known := execResultRowCount(res); known {
						v.Rows = &n
						if n == 0 {
							v.Status, bad = "empty", bad+1
						}
					}
				}
				verdicts = append(verdicts, v)
			}

			if jsonOut {
				if err := emitJSON(struct {
					Dashboard string         `json:"dashboard"`
					Charts    []chartVerdict `json:"charts"`
					Bad       int            `json:"bad"`
				}{id, verdicts, bad}); err != nil {
					return err
				}
			} else {
				tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "STATUS\tROWS\tCHART\tTITLE")
				for _, v := range verdicts {
					rows := "-"
					if v.Rows != nil {
						rows = fmt.Sprintf("%d", *v.Rows)
					}
					line := fmt.Sprintf("%s\t%s\t%s\t%s", strings.ToUpper(v.Status), rows, v.ChartID, truncate(v.Title, 40))
					if v.Error != "" {
						line += " — " + truncate(v.Error, 60)
					}
					fmt.Fprintln(tw, line)
				}
				if err := tw.Flush(); err != nil {
					return err
				}
				fmt.Printf("\n%d chart(s), %d need attention (empty/error).\n", len(verdicts), bad)
			}
			if bad > 0 {
				return divergence("dashboard %s: %d chart(s) empty or errored", id, bad)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearCache, "clear-cache", false, "bypass the query cache (read from the database)")
	return markJSON(cmd)
}

// parseFilterArg validates an optional --filter value as a JSON array (or a single
// JSON object, wrapped into a one-element array) and returns the filter slice.
func parseFilterArg(s string) ([]json.RawMessage, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("--filter is not valid JSON: %s", s)
	}
	var arr []json.RawMessage
	if json.Unmarshal([]byte(s), &arr) == nil {
		return arr, nil
	}
	return []json.RawMessage{json.RawMessage(s)}, nil
}
