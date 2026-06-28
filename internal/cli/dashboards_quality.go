package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// dashboardQuality registers the lint / fix / inspect commands on the
// dashboards group — static analysis + auto-fix + raw chart debugging.

func init() {
	// Registered via newDashboardsCmd in dashboards.go — see addQualityCommands.
}

// addQualityCommands wires lint / fix / inspect into the dashboards group.
func addQualityCommands(parent *cobra.Command) {
	parent.AddCommand(
		newDashboardsInspectCmd(),
		newDashboardsLintCmd(),
		newDashboardsFixCmd(),
	)
}

// ── inspect ──────────────────────────────────────────────────────────

// inspectChart is the per-chart record emitted by `dashboards inspect`.
type inspectChart struct {
	ChartID       string          `json:"chartId"`
	Title         string          `json:"title"`
	TileType      string          `json:"tileType"`
	DataSources   []string        `json:"dataSources,omitempty"`
	Visualization json.RawMessage `json:"visualization,omitempty"`
	QueryID       string          `json:"queryId,omitempty"`
	Query         string          `json:"query,omitempty"`
	QueryInput    json.RawMessage `json:"queryInput,omitempty"`
	Layout        json.RawMessage `json:"layout,omitempty"`
	Error         string          `json:"error,omitempty"`
}

func newDashboardsInspectCmd() *cobra.Command {
	var chartIDFlag string
	cmd := &cobra.Command{
		Use:   "inspect <dashboard-id>",
		Short: "Show raw chart details: visualization, query, layout, datasource (read-only)",
		Long: "Diagnostic view of a dashboard's charts — prints the visualization JSON,\n" +
			"YARA-L query body, query input (time range), layout position, and datasource\n" +
			"binding for each chart. A debugging complement to `export`: shows individual\n" +
			"chart server state without the full export envelope. Use --chart-id to narrow\n" +
			"to a single chart. Read-only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dashID := args[0]
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			full, err := c.GetDashboard(ctx, dashID, true)
			if err != nil {
				return err
			}
			refs, layouts := chartRefsAndLayouts(full.Raw)

			if chartIDFlag != "" {
				refs = filterRefs(refs, chartIDFlag)
				if len(refs) == 0 {
					return fmt.Errorf("chart %s not found on dashboard %s", chartIDFlag, dashID)
				}
			}

			charts := c.ChartsByID(ctx, refs)
			var views []inspectChart
			for _, ref := range refs {
				cid := lastSegment(ref)
				v := inspectChart{ChartID: cid, Layout: layouts[cid]}
				raw, ok := charts[cid]
				if !ok {
					v.Error = "chart not found (dangling reference)"
					views = append(views, v)
					continue
				}
				v.Title, v.TileType, v.DataSources, v.Visualization = parseChartFields(raw)
				qRef := nestedString(raw, "chartDatasource", "dashboardQuery")
				if qRef != "" {
					v.QueryID = lastSegment(qRef)
					if qraw, qerr := c.GetQuery(ctx, qRef); qerr == nil {
						v.Query = nestedString(qraw, "query")
						v.QueryInput = extractRaw(qraw, "input")
					}
				}
				views = append(views, v)
			}

			if jsonOut {
				return emitJSON(views)
			}
			for i, v := range views {
				if i > 0 {
					fmt.Println()
				}
				fmt.Printf("─── %s  %s (%s)\n", v.ChartID, v.Title, v.TileType)
				if v.Error != "" {
					fmt.Printf("  error: %s\n", v.Error)
					continue
				}
				if len(v.DataSources) > 0 {
					fmt.Printf("  datasources: %s\n", strings.Join(v.DataSources, ", "))
				}
				if len(v.Layout) > 0 {
					fmt.Printf("  layout: %s\n", string(v.Layout))
				}
				if len(v.Visualization) > 0 && string(v.Visualization) != "{}" {
					fmt.Printf("  visualization:\n%s\n", indentJSONPrefixed(v.Visualization, "    "))
				}
				if v.Query != "" {
					fmt.Printf("  query:\n%s\n", indentLines(v.Query, "    "))
				}
				if len(v.QueryInput) > 0 {
					fmt.Printf("  input: %s\n", string(v.QueryInput))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&chartIDFlag, "chart-id", "", "narrow to a single chart id")
	return markJSON(cmd)
}

// ── lint ─────────────────────────────────────────────────────────────

// lintFinding is one issue found on a chart.
type lintFinding struct {
	ChartID string `json:"chartId"`
	Title   string `json:"title"`
	Check   string `json:"check"`
	Message string `json:"message"`
	Fixable bool   `json:"fixable"`
}

func newDashboardsLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint <dashboard-id>",
		Short: "Static analysis of a dashboard's charts — check for common issues (read-only)",
		Long: "Checks every chart for:\n" +
			"  1. \"none\" legend — a legends array on a single-series chart renders\n" +
			"     \"none\" as the label in the console (fixable: remove legends).\n" +
			"  2. Long axis labels — email addresses / FQDNs without re.capture()\n" +
			"     truncation, unreadable at >30 chars (fixable: --strip-domain).\n" +
			"  3. Time-range desync — per-chart query input.relativeTime differs\n" +
			"     from the dashboard's global time filter (fixable: --sync-time).\n" +
			"  4. Missing chart title — untitled charts are hard to identify.\n" +
			"  5. Overlapping grid positions — two charts occupying the same cells.\n" +
			"Read-only — no API writes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dashID := args[0]
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			full, err := c.GetDashboard(ctx, dashID, true)
			if err != nil {
				return err
			}
			refs, layouts := chartRefsAndLayouts(full.Raw)
			dashFilter := dashboardGlobalTimeFilter(full.Raw)

			charts := c.ChartsByID(ctx, refs)
			var findings []lintFinding

			for _, ref := range refs {
				cid := lastSegment(ref)
				raw, ok := charts[cid]
				if !ok {
					continue
				}
				title, _, _, viz := parseChartFields(raw)
				qRef := nestedString(raw, "chartDatasource", "dashboardQuery")
				var query string
				var queryInput json.RawMessage
				if qRef != "" {
					if qraw, qerr := c.GetQuery(ctx, qRef); qerr == nil {
						query = nestedString(qraw, "query")
						queryInput = extractRaw(qraw, "input")
					}
				}

				// Check 1: "none" legend on single-series charts.
				if hasNoneLegend(viz, query) {
					findings = append(findings, lintFinding{
						ChartID: cid, Title: title, Check: "none-legend",
						Message: "legends array on a single-series chart renders \"none\" label",
						Fixable: true,
					})
				}

				// Check 2: long axis labels (email addresses without re.capture).
				if hasLongEmailLabels(query) {
					findings = append(findings, lintFinding{
						ChartID: cid, Title: title, Check: "long-labels",
						Message: "email match variable without re.capture() — labels include @domain",
						Fixable: true,
					})
				}

				// Check 3: time-range desync.
				if dashFilter != "" && len(queryInput) > 0 {
					chartTime := normalizeTimeRange(queryInput)
					if chartTime != "" && chartTime != dashFilter {
						findings = append(findings, lintFinding{
							ChartID: cid, Title: title, Check: "time-desync",
							Message: fmt.Sprintf("chart time %s differs from dashboard filter %s", chartTime, dashFilter),
							Fixable: true,
						})
					}
				}

				// Check 4: missing title.
				if strings.TrimSpace(title) == "" {
					findings = append(findings, lintFinding{
						ChartID: cid, Title: "(untitled)", Check: "missing-title",
						Message: "chart has no display name",
						Fixable: false,
					})
				}

				_ = layouts // used by overlap check below
			}

			// Check 5: overlapping grid positions.
			findings = append(findings, checkOverlaps(refs, layouts, charts)...)

			if jsonOut {
				return emitJSON(findings)
			}
			if len(findings) == 0 {
				fmt.Printf("Dashboard %s — no issues found.\n", dashID)
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "CHART\tTITLE\tCHECK\tFIXABLE\tMESSAGE")
			for _, f := range findings {
				fix := ""
				if f.Fixable {
					fix = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					truncate(f.ChartID, 12), truncate(f.Title, 30), f.Check, fix, f.Message)
			}
			_ = tw.Flush()
			fmt.Printf("\n%d finding(s).\n", len(findings))
			return nil
		},
	}
	return markJSON(cmd)
}

// ── fix ──────────────────────────────────────────────────────────────

func newDashboardsFixCmd() *cobra.Command {
	var stripDomain, noLegend, syncTime, dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "fix <dashboard-id>",
		Short: "Auto-fix lint findings on a dashboard's charts (guarded)",
		Long: "Apply mechanical fixes to a dashboard's charts:\n" +
			"  --strip-domain   wrap email-address match variables in\n" +
			"                   re.capture(…, \"^([^@]+)\") to drop @domain from labels.\n" +
			"  --no-legend      remove the legends array from single-series charts.\n" +
			"  --sync-time      align per-chart query time ranges with the dashboard's\n" +
			"                   global time filter.\n" +
			"At least one flag is required. Guarded: dry-run by default, --yes to apply.\n" +
			"Each fix re-reads the chart first (etag-safe).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stripDomain && !noLegend && !syncTime {
				return fmt.Errorf("at least one fix flag is required: --strip-domain, --no-legend, --sync-time")
			}
			dashID := args[0]
			fixes := []string{}
			if stripDomain {
				fixes = append(fixes, "strip-domain")
			}
			if noLegend {
				fixes = append(fixes, "no-legend")
			}
			if syncTime {
				fixes = append(fixes, "sync-time")
			}

			target := fmt.Sprintf("fix dashboard %s (%s)", dashID, strings.Join(fixes, ", "))
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				// Fall through to show what WOULD be fixed.
			}
			if !dr && !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to fix without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			full, err := c.GetDashboard(ctx, dashID, true)
			if err != nil {
				return err
			}
			refs, _ := chartRefsAndLayouts(full.Raw)
			dashFilter := dashboardGlobalTimeFilter(full.Raw)

			charts := c.ChartsByID(ctx, refs)
			type fixAction struct {
				chartID string
				title   string
				what    string
			}
			var actions []fixAction

			for _, ref := range refs {
				cid := lastSegment(ref)
				raw, ok := charts[cid]
				if !ok {
					continue
				}
				title, _, _, viz := parseChartFields(raw)
				qRef := nestedString(raw, "chartDatasource", "dashboardQuery")
				var query string
				var queryInput json.RawMessage
				if qRef != "" {
					if qraw, qerr := c.GetQuery(ctx, qRef); qerr == nil {
						query = nestedString(qraw, "query")
						queryInput = extractRaw(qraw, "input")
					}
				}

				if noLegend && hasNoneLegend(viz, query) {
					actions = append(actions, fixAction{cid, title, "no-legend"})
				}
				if stripDomain && hasLongEmailLabels(query) {
					actions = append(actions, fixAction{cid, title, "strip-domain"})
				}
				if syncTime && dashFilter != "" && len(queryInput) > 0 {
					chartTime := normalizeTimeRange(queryInput)
					if chartTime != "" && chartTime != dashFilter {
						actions = append(actions, fixAction{cid, title, "sync-time"})
					}
				}
			}

			if len(actions) == 0 {
				if jsonOut {
					return emitJSON([]any{})
				}
				fmt.Println("No fixable issues found.")
				return nil
			}

			if dr {
				fmt.Printf("DRY RUN — would apply %d fix(es):\n", len(actions))
				for _, a := range actions {
					fmt.Printf("  • [%s] %s — %s\n", truncate(a.chartID, 12), a.title, a.what)
				}
				fmt.Println("Re-run with --yes to apply.")
				return nil
			}

			applied := 0
			for _, a := range actions {
				var fixErr error
				switch a.what {
				case "no-legend":
					fixErr = applyNoLegend(ctx, c, dashID, a.chartID)
				case "strip-domain":
					fixErr = applyStripDomain(ctx, c, dashID, a.chartID)
				case "sync-time":
					fixErr = applySyncTime(ctx, c, dashID, a.chartID, full.Raw)
				}
				if fixErr != nil {
					fmt.Fprintf(os.Stderr, "  fix %s on %s failed: %v\n", a.what, a.chartID, fixErr)
					continue
				}
				applied++
				fmt.Printf("  fixed [%s] %s — %s\n", truncate(a.chartID, 12), a.title, a.what)
			}
			fmt.Printf("\n%d/%d fix(es) applied. Re-pull to mirror changes locally.\n", applied, len(actions))
			return nil
		},
	}
	cmd.Flags().BoolVar(&stripDomain, "strip-domain", false, "wrap email match variables in re.capture() to drop @domain")
	cmd.Flags().BoolVar(&noLegend, "no-legend", false, "remove legends array from single-series charts")
	cmd.Flags().BoolVar(&syncTime, "sync-time", false, "align per-chart time ranges with the dashboard global filter")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// ── lint helpers ─────────────────────────────────────────────────────

// chartRefsAndLayouts parses a FULL-view dashboard's definition.charts, returning
// the ordered chart resource-name refs and a map of chartID → layout JSON.
func chartRefsAndLayouts(dashRaw json.RawMessage) (refs []string, layouts map[string]json.RawMessage) {
	layouts = map[string]json.RawMessage{}
	var def struct {
		Definition struct {
			Charts []struct {
				DashboardChart string          `json:"dashboardChart"`
				ChartLayout    json.RawMessage `json:"chartLayout"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if json.Unmarshal(dashRaw, &def) != nil {
		return nil, layouts
	}
	for _, ch := range def.Definition.Charts {
		if ch.DashboardChart != "" {
			refs = append(refs, ch.DashboardChart)
			layouts[lastSegment(ch.DashboardChart)] = ch.ChartLayout
		}
	}
	return refs, layouts
}

func filterRefs(refs []string, id string) []string {
	want := lastSegment(id)
	for _, r := range refs {
		if lastSegment(r) == want {
			return []string{r}
		}
	}
	return nil
}

// parseChartFields extracts the typed fields from a chart's raw JSON.
func parseChartFields(raw json.RawMessage) (title, tileType string, dataSources []string, viz json.RawMessage) {
	var ch struct {
		DisplayName     string          `json:"displayName"`
		TileType        string          `json:"tileType"`
		Visualization   json.RawMessage `json:"visualization"`
		ChartDatasource struct {
			DataSources []string `json:"dataSources"`
		} `json:"chartDatasource"`
	}
	_ = json.Unmarshal(raw, &ch)
	return ch.DisplayName, ch.TileType, ch.ChartDatasource.DataSources, ch.Visualization
}

// extractRaw pulls a top-level key from raw JSON as a RawMessage.
func extractRaw(raw json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		return m[key]
	}
	return nil
}

// indentJSONPrefixed pretty-prints raw JSON with a per-line prefix.
func indentJSONPrefixed(raw json.RawMessage, prefix string) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return prefix + string(raw)
	}
	b, _ := json.MarshalIndent(v, prefix, "  ")
	return prefix + string(b)
}

// hasNoneLegend detects a legends array on a single-match-variable chart.
func hasNoneLegend(viz json.RawMessage, query string) bool {
	if len(viz) == 0 {
		return false
	}
	var v struct {
		Legends []json.RawMessage `json:"legends"`
	}
	if json.Unmarshal(viz, &v) != nil || len(v.Legends) == 0 {
		return false
	}
	matchVars := countMatchVars(query)
	return matchVars <= 1
}

// countMatchVars counts the variables in the match: section of a YARA-L stats query.
func countMatchVars(query string) int {
	lines := strings.Split(query, "\n")
	inMatch := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "match:") {
			inMatch = true
			continue
		}
		if inMatch {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "outcome:") || strings.HasPrefix(trimmed, "order:") ||
				strings.HasPrefix(trimmed, "limit:") || strings.HasPrefix(trimmed, "condition:") {
				break
			}
			if strings.HasPrefix(trimmed, "$") || strings.Contains(trimmed, ",") {
				for p := range strings.SplitSeq(trimmed, ",") {
					if strings.TrimSpace(p) != "" {
						count++
					}
				}
			}
		}
	}
	return count
}

// emailMatchRE detects a match variable assigned from an email field without re.capture.
var emailMatchRE = regexp.MustCompile(
	`\$\w+\s*=\s*(?:principal|target|src|observer|intermediary|about)\.\w*\.?(?:email_addresses|userid|email)\b`)

// reCaptureRE detects re.capture already wrapping the assignment.
var reCaptureRE = regexp.MustCompile(`re\.capture\(`)

// hasLongEmailLabels checks if a query has email match variables without re.capture.
func hasLongEmailLabels(query string) bool {
	if query == "" {
		return false
	}
	for line := range strings.SplitSeq(query, "\n") {
		if emailMatchRE.MatchString(line) && !reCaptureRE.MatchString(line) {
			return true
		}
	}
	return false
}

// dashboardGlobalTimeFilter extracts the normalized time range string from a
// dashboard's global filter, e.g. "14-DAY" or "24-HOUR".
func dashboardGlobalTimeFilter(dashRaw json.RawMessage) string {
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
	if json.Unmarshal(dashRaw, &def) != nil {
		return ""
	}
	for _, f := range def.Definition.Filters {
		if f.IsStandardTimeRangeFilter || f.ID == "GlobalTimeFilter" {
			for _, op := range f.FilterOperatorAndFieldValues {
				if len(op.FieldValues) >= 2 {
					return op.FieldValues[0] + "-" + op.FieldValues[1]
				}
			}
		}
	}
	return ""
}

// normalizeTimeRange turns a query input.relativeTime into "N-UNIT", e.g. "1-DAY".
func normalizeTimeRange(input json.RawMessage) string {
	var inp struct {
		RelativeTime struct {
			TimeUnit     string `json:"timeUnit"`
			StartTimeVal string `json:"startTimeVal"`
		} `json:"relativeTime"`
	}
	if json.Unmarshal(input, &inp) != nil {
		return ""
	}
	if inp.RelativeTime.TimeUnit == "" || inp.RelativeTime.StartTimeVal == "" {
		return ""
	}
	return inp.RelativeTime.StartTimeVal + "-" + inp.RelativeTime.TimeUnit
}

// checkOverlaps detects charts whose grid rectangles overlap.
func checkOverlaps(refs []string, layouts, charts map[string]json.RawMessage) []lintFinding {
	type rect struct {
		id, title                    string
		startX, startY, spanX, spanY int
	}
	var rects []rect
	for _, ref := range refs {
		cid := lastSegment(ref)
		lay, ok := layouts[cid]
		if !ok || len(lay) == 0 {
			continue
		}
		var r struct {
			StartX int `json:"startX"`
			StartY int `json:"startY"`
			SpanX  int `json:"spanX"`
			SpanY  int `json:"spanY"`
		}
		if json.Unmarshal(lay, &r) != nil {
			continue
		}
		title := ""
		if raw, ok := charts[cid]; ok {
			title, _, _, _ = parseChartFields(raw)
		}
		rects = append(rects, rect{cid, title, r.StartX, r.StartY, r.SpanX, r.SpanY})
	}

	var findings []lintFinding
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			a, b := rects[i], rects[j]
			if a.startX < b.startX+b.spanX && a.startX+a.spanX > b.startX &&
				a.startY < b.startY+b.spanY && a.startY+a.spanY > b.startY {
				findings = append(findings, lintFinding{
					ChartID: a.id, Title: a.title, Check: "overlap",
					Message: fmt.Sprintf("overlaps with chart %s (%s)", truncate(b.id, 12), b.title),
					Fixable: false,
				})
			}
		}
	}
	return findings
}
