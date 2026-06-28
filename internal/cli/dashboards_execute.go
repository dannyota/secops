package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"danny.vn/secops/chronicle"
)

// defaultVerifyConcurrency bounds how many charts `verify` executes at once. A
// chart costs two API calls (GetChart + dashboardQueries:execute); running them in
// parallel turns a serial N-chart verify into roughly N/concurrency round-trips
// (a many-chart dashboard drops from minutes to seconds). Capped so a big dashboard
// can't burst past the per-minute quota (the transport still retries any 429).
const defaultVerifyConcurrency = 8

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
// executes it — the by-chart-id convenience for `charts run`.
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
		Use:     "run <dashboard-id> --chart-id <id>",
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

// verifyOneChart reads a chart's title + query ref (GetChart) and executes the
// query (dashboardQueries:execute), returning a verdict. Errors are captured into
// the verdict (never returned) so a single bad chart doesn't abort the whole
// verify — the report lists every chart. Safe to call concurrently: it shares only
// the (concurrency-safe) client and writes nothing shared.
func verifyOneChart(ctx context.Context, c *chronicle.Client, ref string, clearCache *bool) chartVerdict {
	v := chartVerdict{ChartID: lastSegment(ref), Status: "ok"}
	chartRaw, gerr := retryTransient(ctx, func() (json.RawMessage, error) { return c.GetChart(ctx, ref) })
	if gerr != nil {
		v.Status, v.Error = classifyChartErr(gerr)
		return v
	}
	v.Title = nestedString(chartRaw, "displayName")
	queryRef := nestedString(chartRaw, "chartDatasource", "dashboardQuery")
	res, eerr := execChartQuery(ctx, c, queryRef, clearCache)
	if eerr != nil {
		v.Status, v.Error = classifyChartErr(eerr)
		return v
	}
	if n, known := execResultRowCount(res); known {
		v.Rows = &n
		if n == 0 {
			v.Status = "empty"
		}
	}
	return v
}

// classifyChartErr separates a genuinely broken chart from a transient failure.
// A genuine break is a 404 (dangling chart/query) or any other 4xx — a 400
// (uncompilable/invalid query) or 403 (access) means the chart really is broken,
// not flaky. A transient failure (429, intermittent 5xx, timeout) must NOT be
// reported as broken, or a flaky run (e.g. `verify --all` under load) wrongly
// condemns a healthy dashboard. The 429 case is transient (retry/quota), not a
// client error.
func classifyChartErr(err error) (status, msg string) {
	if chronicle.IsNotFound(err) {
		return "error", err.Error()
	}
	var ae *chronicle.APIError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 && ae.Status != 429 {
		return "error", reframeChartErr(ae)
	}
	return "transient", err.Error()
}

// reframeChartErr rewrites opaque YARA-L compile errors into actionable messages.
// The most common: "no viable alternative at input '$<tok>'" means a $variable
// name collides with a reserved keyword — rename it (e.g. $rule → $rule_v).
func reframeChartErr(ae *chronicle.APIError) string {
	_, after, found := strings.Cut(ae.Body, "no viable alternative at input '")
	if found {
		tok, _, _ := strings.Cut(after, "'")
		return fmt.Sprintf("reserved-word variable %s — rename it (e.g. %s → %s_v) and re-add the chart", tok, tok, tok)
	}
	return ae.Error()
}

// retryTransient runs fn, retrying a transient (non-404) failure a few times with
// backoff. The chronicle host returns intermittent 5xx under load — especially when
// `verify --all` fans out hundreds of calls — and the chart execute is a POST the
// transport won't auto-retry on 5xx, so a healthy chart/dashboard can momentarily
// fail. A 404 (a genuinely missing resource) is returned immediately, never retried.
func retryTransient[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	const attempts = 3
	var res T
	var err error
	for attempt := range attempts {
		if res, err = fn(); err == nil || chronicle.IsNotFound(err) {
			return res, err
		}
		if attempt == attempts-1 {
			break // don't back off after the final attempt — it's wasted wall-clock
		}
		select {
		case <-ctx.Done():
			return res, err
		case <-time.After(time.Duration(attempt+1) * 400 * time.Millisecond):
		}
	}
	return res, err
}

// execChartQuery executes a chart's query with transient-retry.
func execChartQuery(ctx context.Context, c *chronicle.Client, queryRef string, clearCache *bool) (json.RawMessage, error) {
	return retryTransient(ctx, func() (json.RawMessage, error) {
		return execQueryRef(ctx, c, queryRef, nil, clearCache)
	})
}

// countVerdicts splits a chart report into genuinely-bad (broken 404 or empty) and
// transient (inconclusive) counts — transient is never counted as broken.
func countVerdicts(vs []chartVerdict) (bad, transient int) {
	for _, v := range vs {
		switch v.Status {
		case "error", "empty":
			bad++
		case "transient":
			transient++
		}
	}
	return bad, transient
}

// verifyDashboard resolves a dashboard's chart refs and executes them in parallel
// (bounded by concurrency), returning the per-chart verdicts (in chart order) and
// the count that are not "ok". A resolve error (e.g. the dashboard 404s) is
// returned; per-chart errors are carried in the verdicts, not returned.
func verifyDashboard(ctx context.Context, c *chronicle.Client, id string, concurrency int, clearCache *bool) (verdicts []chartVerdict, bad, transient int, err error) {
	if concurrency < 1 {
		concurrency = 1
	}
	// Retry the dashboard-level fetch too — under `verify --all` load it can hit the
	// same transient 5xx as the chart executes; a real "gone" dashboard 404s and
	// returns immediately.
	refs, err := retryTransient(ctx, func() ([]string, error) {
		return dashboardChartRefs(ctx, c, id)
	})
	if err != nil {
		return nil, 0, 0, err
	}
	// Execute charts in parallel (bounded), writing each verdict to its own slot so
	// the report stays in chart order and no lock is needed.
	verdicts = make([]chartVerdict, len(refs))
	var g errgroup.Group
	g.SetLimit(concurrency)
	for i, ref := range refs {
		g.Go(func() error {
			verdicts[i] = verifyOneChart(ctx, c, ref, clearCache)
			return nil
		})
	}
	_ = g.Wait() // verdicts carry their own errors; Wait never returns one
	bad, transient = countVerdicts(verdicts)
	return verdicts, bad, transient, nil
}

func newDashboardsVerifyCmd() *cobra.Command {
	var clearCache bool
	var concurrency int
	var all, includeCurated bool
	cmd := &cobra.Command{
		Use:   "verify [<dashboard-id>]",
		Short: "Execute every chart and flag the ones returning no rows or an error (read-only)",
		Long: "A dashboard health check: execute each chart's query (`dashboardQueries:execute`)\n" +
			"and report which charts return an ERROR or 0 rows (EMPTY) vs OK — so a blank or\n" +
			"broken chart is caught headless / in CI without opening the UI. Charts run in\n" +
			"parallel (--concurrency, default 8) so a many-chart dashboard verifies in\n" +
			"seconds. Pass --all to health-check every dashboard in the instance (a fleet\n" +
			"rollup; one row per dashboard). Read-only; exits non-zero (2) when any chart is\n" +
			"empty or errored. --json for the full report.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if concurrency < 1 {
				concurrency = 1
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			var cc *bool
			if clearCache {
				cc = &clearCache
			}

			if all {
				if len(args) > 0 {
					return fmt.Errorf("pass either a dashboard id or --all, not both")
				}
				return runVerifyAll(ctx, c, concurrency, cc, includeCurated)
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a dashboard id, or pass --all to verify every dashboard")
			}
			id := args[0]
			verdicts, bad, transient, err := verifyDashboard(ctx, c, id, concurrency, cc)
			if err != nil {
				return err
			}

			if jsonOut {
				if err := emitJSON(struct {
					Dashboard string         `json:"dashboard"`
					Charts    []chartVerdict `json:"charts"`
					Bad       int            `json:"bad"`
					Transient int            `json:"transient"`
				}{id, verdicts, bad, transient}); err != nil {
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
				note := ""
				if transient > 0 {
					note = fmt.Sprintf(" (%d transient — inconclusive, re-run)", transient)
				}
				fmt.Printf("\n%d chart(s), %d need attention (empty/error)%s.\n", len(verdicts), bad, note)
			}
			if bad > 0 {
				return divergence("dashboard %s: %d chart(s) empty or errored", id, bad)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearCache, "clear-cache", false, "bypass the query cache (read from the database)")
	cmd.Flags().IntVar(&concurrency, "concurrency", defaultVerifyConcurrency, "max charts to execute in parallel (lower it if you hit rate limits)")
	cmd.Flags().BoolVar(&all, "all", false, "health-check every dashboard in the instance (fleet rollup, one row per dashboard)")
	cmd.Flags().BoolVar(&includeCurated, "include-curated", false, "with --all: also verify CURATED (Google-managed) dashboards, not just your CUSTOM ones")
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
