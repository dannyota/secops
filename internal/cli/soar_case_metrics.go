package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// Case queue metrics: per-analyst workload, aging open cases, and MTTR-style
// stats. These compute from the case list (createTime/updateTime epoch-millis,
// assignee, sla) the modern cases API returns — the manager/triage view the
// per-case verbs don't give. Read-only.

// caseMeta pulls the timestamp + SLA fields the typed soar.Case does not model
// out of its Raw payload.
func caseMeta(cs *soar.Case) (create, update time.Time, sla string) {
	var m struct {
		CreateTime json.RawMessage `json:"createTime"`
		UpdateTime json.RawMessage `json:"updateTime"`
		SLA        struct {
			ExpirationStatus string `json:"expirationStatus"`
		} `json:"sla"`
	}
	_ = json.Unmarshal(cs.Raw, &m)
	return parseCaseTime(m.CreateTime), parseCaseTime(m.UpdateTime), m.SLA.ExpirationStatus
}

// parseCaseTime tolerates both timestamp shapes the cases API has used: an
// epoch-millis number (the SOAR-host modern cases payload) and an RFC3339
// string. Returns the zero time when absent or unparseable.
func parseCaseTime(raw json.RawMessage) time.Time {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 0 {
		return time.UnixMilli(ms).UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// fetchCases fetches up to maxItems cases for the given server-side filter,
// expanding the SLA fields the metrics read.
func fetchCases(filter string, maxItems int) ([]soar.Case, error) {
	c, err := newSOARClient()
	if err != nil {
		return nil, err
	}
	return c.ListCasesTyped(baseContext(), soar.CaseListOptions{
		PageSize: 1000,
		MaxItems: maxItems,
		Filter:   filter,
		Expand:   "sla,alertsSla,tags",
		OrderBy:  "createTime desc",
	})
}

func caseID(cs *soar.Case) string {
	if cs.DisplayID != "" {
		return cs.DisplayID
	}
	if i := len(cs.Name); i > 0 {
		for j := i - 1; j >= 0; j-- {
			if cs.Name[j] == '/' {
				return cs.Name[j+1:]
			}
		}
	}
	return cs.Name
}

func newCaseWorkloadCmd() *cobra.Command {
	var filter string
	var maxItems int
	cmd := &cobra.Command{
		Use:   "workload [--filter <expr>]",
		Short: "Read-only: open-case load per analyst (queue distribution)",
		Long: "Group open cases by assignee and count them — who is carrying how much of\n" +
			"the queue, for load-balancing and shift planning. --filter composes with the\n" +
			"open-status base (e.g. \"priority = 'PRIORITY_HIGH'\"). JSON or table.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateCaseFilter(filter); err != nil {
				return err
			}
			cases, err := fetchCases(joinFilter("status = 'OPENED'", filter), maxItems)
			if err != nil {
				return err
			}
			counts := map[string]int{}
			for i := range cases {
				a := cases[i].Assignee
				if a == "" {
					a = "(unassigned)"
				}
				counts[a]++
			}
			type row struct {
				Assignee string `json:"assignee"`
				Open     int    `json:"open_cases"`
			}
			rows := make([]row, 0, len(counts))
			for a, n := range counts {
				rows = append(rows, row{a, n})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Open > rows[j].Open })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ASSIGNEE\tOPEN CASES")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%d\n", r.Assignee, r.Open)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d open case(s) across %d assignee(s).\n", len(cases), len(rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "extra server-side filter (composed with status = 'OPENED')")
	cmd.Flags().IntVar(&maxItems, "max", 2000, "max cases to scan")
	return markJSON(cmd)
}

func newCaseAgingCmd() *cobra.Command {
	var filter string
	var limit, maxItems int
	cmd := &cobra.Command{
		Use:   "aging [--filter <expr>] [--limit N]",
		Short: "Read-only: list open cases by age (oldest first) with SLA status",
		Long: "List open cases oldest-first by age (now − createTime), with priority and SLA\n" +
			"expiration status — the queue-health view for spotting stale/aging cases.\n" +
			"--filter composes with the open-status base. JSON or table.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateCaseFilter(filter); err != nil {
				return err
			}
			cases, err := fetchCases(joinFilter("status = 'OPENED'", filter), maxItems)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			type row struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Priority string `json:"priority"`
				AgeHours int    `json:"age_hours"`
				SLA      string `json:"sla"`
			}
			rows := make([]row, 0, len(cases))
			for i := range cases {
				create, _, sla := caseMeta(&cases[i])
				age := 0
				if !create.IsZero() {
					age = int(now.Sub(create).Hours())
				}
				rows = append(rows, row{caseID(&cases[i]), cases[i].Title, prettyPriority(cases[i].Priority), age, orDash(sla)})
			}
			slices.SortStableFunc(rows, func(a, b row) int { return b.AgeHours - a.AgeHours })
			if limit > 0 && len(rows) > limit {
				rows = rows[:limit]
			}
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tAGE(h)\tPRIORITY\tSLA\tTITLE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", r.ID, r.AgeHours, r.Priority, r.SLA, truncate(r.Title, 48))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "extra server-side filter (composed with status = 'OPENED')")
	cmd.Flags().IntVar(&limit, "limit", 25, "max rows to show (0 = all)")
	cmd.Flags().IntVar(&maxItems, "max", 2000, "max cases to scan")
	return markJSON(cmd)
}

func newCaseStatsCmd() *cobra.Command {
	var filter string
	var maxItems int
	cmd := &cobra.Command{
		Use:   "stats [--filter <expr>]",
		Short: "Read-only: show queue stats — open/closed counts, age + resolution-time percentiles",
		Long: "Compute queue health over the scanned cases: open vs closed counts, open-case\n" +
			"age p50/p90 (now − createTime), and closed-case resolution-time p50/p90\n" +
			"(updateTime − createTime, a close-time proxy — the case payload has no\n" +
			"separate detection/close timestamp, so this is resolution, not MTTD). --filter\n" +
			"narrows the scanned set. JSON or table.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateCaseFilter(filter); err != nil {
				return err
			}
			cases, err := fetchCases(filter, maxItems)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			var openAges, closedRes []float64
			open, closed := 0, 0
			for i := range cases {
				create, update, _ := caseMeta(&cases[i])
				if create.IsZero() {
					continue
				}
				if cases[i].Status == "CLOSED" {
					closed++
					if !update.IsZero() {
						closedRes = append(closedRes, update.Sub(create).Hours())
					}
				} else {
					open++
					openAges = append(openAges, now.Sub(create).Hours())
				}
			}
			out := map[string]any{
				"scanned":              len(cases),
				"open":                 open,
				"closed":               closed,
				"open_age_hours_p50":   pct(openAges, 0.5),
				"open_age_hours_p90":   pct(openAges, 0.9),
				"resolution_hours_p50": pct(closedRes, 0.5),
				"resolution_hours_p90": pct(closedRes, 0.9),
			}
			if jsonOut {
				return emitJSON(out)
			}
			fmt.Fprintf(os.Stdout, "Scanned %d case(s): %d open, %d closed.\n", len(cases), open, closed)
			fmt.Fprintf(os.Stdout, "Open age (h):        p50 %.1f   p90 %.1f\n", pct(openAges, 0.5), pct(openAges, 0.9))
			fmt.Fprintf(os.Stdout, "Resolution time (h): p50 %.1f   p90 %.1f  (create→close proxy)\n", pct(closedRes, 0.5), pct(closedRes, 0.9))
			return nil
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "server-side filter to scope the scanned cases")
	cmd.Flags().IntVar(&maxItems, "max", 2000, "max cases to scan")
	return markJSON(cmd)
}

// pct returns the p-quantile (0..1) of xs, or 0 for an empty set.
func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(p * float64(len(s)-1))
	return s[i]
}

// joinFilter composes a base filter with an optional extra clause (AND).
func joinFilter(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " AND (" + extra + ")"
}
