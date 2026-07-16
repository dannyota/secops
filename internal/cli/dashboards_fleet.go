package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Fleet-level dashboard ops: `dashboards list` (discover id ↔ name without digging
// through pulled files) and `dashboards verify --all` (health-check every dashboard
// in one call). Both are read-only.

// newDashboardsDeleteCmd deletes a whole dashboard by id — e.g. a stale
// duplicate. Guarded; prints the title it is about to delete. A corrupt
// dashboard whose definition.charts hold dangling/non-owned references (charts
// that belong to another dashboard or no longer exist) cannot be deleted by the
// API OR the web console — the backend 500s — so on that failure the error is
// augmented with a clear diagnosis.
func newDashboardsDeleteCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <dashboard-id>",
		Short: "Delete a whole dashboard by id (guarded) — e.g. a stale duplicate",
		Long: "Delete a native dashboard by id. Resolves and prints the title before acting.\n" +
			"Guarded: dry-run by default, --yes to apply. This removes the whole dashboard,\n" +
			"not individual charts (see `charts remove`). Note: a corrupt dashboard whose\n" +
			"charts are dangling/non-owned references (charts owned by another dashboard or\n" +
			"already gone) cannot be deleted by the API or the web console; the backend\n" +
			"returns a 500. The error explains this when it happens.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := "delete dashboard " + id
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				ctx := baseContext()
				// Name the dashboard before deleting so the result is unambiguous.
				title := id
				if d, gerr := c.GetDashboard(ctx, id, false); gerr == nil && d.DisplayName != "" {
					title = d.DisplayName
				}
				if err := c.DeleteDashboard(ctx, id); err != nil {
					return hintDeleteDashboard(id, err)
				}
				if !jsonOut {
					fmt.Printf("Deleted dashboard %q (%s).\n", title, id)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// hintDeleteDashboard augments a delete failure with a diagnosis. A 500 deleting a
// dashboard is the known signature of a corrupt dashboard whose charts are
// dangling or owned by another dashboard (a chart belongs to exactly one
// dashboard and can't be shared). The backend cannot cascade such charts, so
// neither the API nor the web console can delete it, and the references can't be
// removed first (removing one dereferences the missing/non-owned chart → 404;
// rewriting definition.charts → 400). The 500 is intrinsic — deleting the chart
// owner first does not unstick it. The hint explains this rather than surfacing a
// bare "INTERNAL"; it is keyed on the 500 status alone (not a per-chart probe —
// those references intermittently read 200 vs 404, so probing them is both
// unreliable and an extra fan-out on every 500).
func hintDeleteDashboard(id string, err error) error {
	var ae *chronicle.APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusInternalServerError {
		return err
	}
	return fmt.Errorf("%w\nhint: the backend returned a 500 deleting dashboard %s. This is the known failure mode for a "+
		"corrupt dashboard whose charts are dangling or owned by another dashboard (a chart belongs to exactly one "+
		"dashboard and can't be shared). Such a dashboard can't be deleted by the API OR the web console, and the corrupt "+
		"state can't be repaired first: its chart references can't be removed (removing one 404s; rewriting the chart list "+
		"400s), and deleting the chart owner does not unstick it. It can only be removed by the platform, so raise it with "+
		"Google support. To preserve the content, recreate it with `dashboards duplicate` from a healthy source. If this "+
		"was a healthy dashboard, the 500 may be transient — retry", err, id)
}

// newDashboardsListCmd lists every native dashboard with enriched columns,
// sort, and search — matching the web console's dashboard table.
func newDashboardsListCmd() *cobra.Command {
	var sortBy, search string
	var desc bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List native dashboards — read-only",
		Long: "List every native dashboard in the instance. Shows id, type, access,\n" +
			"created, updated, and title. Use --sort to order by column\n" +
			"(name, type, access, created, updated); --desc for reverse.\n" +
			"Use --search to filter by title substring. --json for the full objects.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			dashes, err := c.ListNativeDashboards(baseContext())
			if err != nil {
				return err
			}
			if search != "" {
				needle := strings.ToLower(search)
				filtered := dashes[:0:0]
				for _, d := range dashes {
					if strings.Contains(strings.ToLower(d.DisplayName), needle) {
						filtered = append(filtered, d)
					}
				}
				dashes = filtered
			}
			sortDashboards(dashes, sortBy, desc)
			if jsonOut {
				return emitJSON(dashes)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTYPE\tACCESS\tCREATED\tUPDATED\tTITLE")
			for i := range dashes {
				d := &dashes[i]
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					lastSegment(d.Name),
					orDash(d.Type),
					shortAccess(d.Access),
					dateOnly(d.CreateTime),
					dateOnly(d.UpdateTime),
					truncate(d.DisplayName, 50),
				)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Printf("\n%d dashboard(s).\n", len(dashes))
			return nil
		},
	}
	cmd.Flags().StringVar(&sortBy, "sort", "name", "sort by: name, type, access, created, updated")
	cmd.Flags().BoolVar(&desc, "desc", false, "sort in descending order")
	cmd.Flags().StringVar(&search, "search", "", "filter by title (case-insensitive substring)")
	return markJSON(cmd)
}

func shortAccess(a string) string {
	switch a {
	case "DASHBOARD_PUBLIC":
		return "public"
	case "DASHBOARD_PRIVATE":
		return "private"
	case "":
		return "-"
	default:
		return a
	}
}

func dateOnly(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return orDash(ts)
}

func sortDashboards(dashes []chronicle.NativeDashboard, by string, desc bool) {
	less := func(i, j int) bool {
		a, b := &dashes[i], &dashes[j]
		switch strings.ToLower(by) {
		case "type":
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			return strings.ToLower(a.DisplayName) < strings.ToLower(b.DisplayName)
		case "access":
			aa, ab := shortAccess(a.Access), shortAccess(b.Access)
			if aa != ab {
				return aa < ab
			}
			return strings.ToLower(a.DisplayName) < strings.ToLower(b.DisplayName)
		case "created":
			return a.CreateTime < b.CreateTime
		case "updated":
			return a.UpdateTime < b.UpdateTime
		default:
			return strings.ToLower(a.DisplayName) < strings.ToLower(b.DisplayName)
		}
	}
	if desc {
		sort.Slice(dashes, func(i, j int) bool { return less(j, i) })
	} else {
		sort.Slice(dashes, less)
	}
}

// fleetVerdict is one dashboard's rollup in `verify --all`.
type fleetVerdict struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Charts    int    `json:"charts"`
	Bad       int    `json:"bad"`
	Transient int    `json:"transient,omitempty"` // inconclusive charts (rate-limit/5xx), not counted as broken
	Status    string `json:"status"`              // ok | broken | error (gone) | recheck (resolve inconclusive)
	Error     string `json:"error,omitempty"`     // dashboard-level failure message
}

// runVerifyAll health-checks every dashboard and prints a one-row-per-dashboard
// rollup, broken ones first. Dashboards are processed sequentially while each
// dashboard's charts run in parallel — so total in-flight work stays bounded by
// --concurrency. By default only CUSTOM dashboards (the ones you author) are
// checked; CURATED (Google-managed) ones are skipped unless includeCurated is set.
// Exits non-zero (2) if any dashboard has a broken/empty chart.
func runVerifyAll(ctx context.Context, c *chronicle.Client, concurrency int, clearCache *bool, includeCurated bool) error {
	all, err := c.ListNativeDashboards(ctx)
	if err != nil {
		return err
	}
	dashes := all[:0:0]
	skipped := 0
	for _, d := range all {
		if !includeCurated && d.Type == "CURATED" {
			skipped++
			continue
		}
		dashes = append(dashes, d)
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "checking %d custom dashboard(s); skipped %d curated (Google-managed) — pass --include-curated to include them.\n", len(dashes), skipped)
	}
	results := make([]fleetVerdict, 0, len(dashes))
	for i := range dashes {
		id := lastSegment(dashes[i].Name)
		fv := fleetVerdict{ID: id, Title: dashes[i].DisplayName}
		verdicts, bad, transient, verr := verifyDashboard(ctx, c, id, concurrency, clearCache)
		switch {
		case verr == nil:
			fv.Charts, fv.Bad, fv.Transient = len(verdicts), bad, transient
			if bad > 0 {
				fv.Status = "broken"
			} else {
				fv.Status = "ok"
			}
		case chronicle.IsNotFound(verr):
			fv.Status, fv.Error = "error", verr.Error() // the dashboard itself is gone
		default:
			fv.Status, fv.Error = "recheck", verr.Error() // transient — couldn't confirm
		}
		results = append(results, fv)
	}

	broken := 0
	for _, r := range results {
		if r.Status == "broken" || r.Status == "error" {
			broken++
		}
	}

	if jsonOut {
		return emitJSON(struct {
			Dashboards []fleetVerdict `json:"dashboards"`
			Broken     int            `json:"broken"`
			Total      int            `json:"total"`
		}{results, broken, len(results)})
	}

	// Most-actionable first: broken/error, then recheck, then ok; ties by title.
	rank := map[string]int{"broken": 0, "error": 1, "recheck": 2, "ok": 3}
	sort.SliceStable(results, func(i, j int) bool {
		if rank[results[i].Status] != rank[results[j].Status] {
			return rank[results[i].Status] < rank[results[j].Status]
		}
		return results[i].Title < results[j].Title
	})
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCHARTS\tDASHBOARD\tID")
	inconclusive := 0
	for _, r := range results {
		var status, detail string
		switch r.Status {
		case "error":
			status, detail = "ERROR", "dashboard gone (404)"
		case "broken":
			status, detail = "BROKEN", fmt.Sprintf("%d/%d bad", r.Bad, r.Charts)
		case "recheck":
			status, detail = "RECHECK", "transient — re-run"
		default:
			status, detail = "OK", fmt.Sprintf("%d ok", r.Charts)
		}
		if r.Transient > 0 {
			detail += fmt.Sprintf(" +%d transient", r.Transient)
		}
		// Mutually exclusive with broken/error so the summary's healthy count
		// (total − broken − inconclusive) can't double-subtract a broken board.
		if r.Status != "broken" && r.Status != "error" && (r.Status == "recheck" || r.Transient > 0) {
			inconclusive++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, detail, truncate(r.Title, 46), r.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	note := ""
	if inconclusive > 0 {
		note = fmt.Sprintf("; %d inconclusive (transient — re-run to confirm)", inconclusive)
	}
	fmt.Printf("\n%d dashboard(s): %d healthy, %d need attention%s.\n", len(results), len(results)-broken-inconclusive, broken, note)
	if broken > 0 {
		return divergence("%d dashboard(s) have empty/errored charts", broken)
	}
	return nil
}
