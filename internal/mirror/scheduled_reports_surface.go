package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// Scheduled dashboard reports (dashboardScheduledReports) as code, on the reconcile
// engine. A report is a recurring delivery of a native dashboard (PDF/CSV/PNG) to
// email/GCS on a cron schedule. Full CRUD: create (server-assigns the id), update
// (PATCH with etag), delete (prune-eligible). The imperative verbs
// trigger/duplicate/fetchHistory are not config and live in the SDK / CLI, not here.
//
// On disk each report is one `<slug>.json`: its canonical config (displayName,
// description, cronDetails, deliveryDetails, format, scopeInfo, userData, and a
// `dashboard` reduced to its {name} reference) plus a reserved `_server` block
// (id + etag). The full live dashboard object the read returns is reduced to its
// resource-name reference so the report's diff tracks "which dashboard + how it is
// delivered", not the dashboard's (separately managed) contents.

// scheduledReportExtraStrip are output-only/volatile keys dropped from the diff
// basis at any depth (beyond the engine's default name/etag/time stripping).
var scheduledReportExtraStrip = []string{
	"status", "createUserId", "updateUserId",
	"lastSuccessfulGeneratedTime", "lastReportGenerationDetail",
}

func scheduledReportsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "scheduled_reports",
		Dir:     DirScheduledReports,
		Product: reconcile.ProductSIEM,
		// Clean delete-by-id (a report is a benign, recreatable schedule) → prune-
		// eligible. The PATCH round-trips an etag for optimistic concurrency.
		Caps: reconcile.Capabilities{PruneEligible: true},

		List:    scheduledReportsList(c),
		LoadDir: loadScheduledReports,
		Write:   writeScheduledReportObject,
		Create:  scheduledReportsCreate(c),
		Update:  scheduledReportsUpdate(c),
		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteScheduledReport(ctx, lastSegment(live.ServerID), live.Etag)
		},
	}
}

func scheduledReportsList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		reports, err := c.ListScheduledReports(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for i := range reports {
			o, berr := scheduledReportObject(reports[i])
			if berr != nil {
				warnf("scheduled_reports: build %s: %v", reports[i].ID(), berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// scheduledReportObject builds the engine object (canonical diff basis + id/etag)
// for a live scheduled report.
func scheduledReportObject(r chronicle.DashboardScheduledReport) (reconcile.Object, error) {
	canon, err := reportCanonical(r.Raw)
	if err != nil {
		return reconcile.Object{}, err
	}
	display := r.DisplayName
	if display == "" {
		display = r.ID()
	}
	if r.Name == "" {
		return reconcile.Object{}, fmt.Errorf("scheduled report has no resource name")
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: r.Name, Etag: r.Etag, Canonical: canon, Raw: r.Raw}, nil
}

// reportCanonical reduces the embedded dashboard to its {name} reference, drops
// the root resource name (identity, carried in ServerID), then strips output-only
// keys and canonicalizes. The root name is removed here, not via extraStrip,
// because the dashboard's nested name (at depth) is the reference and must be
// preserved; Canonicalize drops etag + time fields.
func reportCanonical(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "name") // root identity → ServerID, not the diff basis
	reduceDashboardRef(m)
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(b, scheduledReportExtraStrip...)
}

// reduceDashboardRef collapses a report's `dashboard` object to just its {name}
// reference (the read returns the full NativeDashboard; only the reference is
// config for the report). A dashboard already given as a bare reference is left
// as-is; a missing/!object dashboard is left untouched.
func reduceDashboardRef(m map[string]any) {
	d, ok := m["dashboard"].(map[string]any)
	if !ok {
		return
	}
	if name, ok := d["name"].(string); ok && name != "" {
		m["dashboard"] = map[string]any{"name": name}
	}
}

func loadScheduledReports(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		id, etag := serverBlock(b)
		canon, cerr := reportCanonical(b)
		if cerr != nil {
			return nil, fmt.Errorf("scheduled_reports: canonicalize %s: %w", e.Name(), cerr)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Etag:      etag,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeScheduledReportObject renders the canonical config plus the `_server`
// identity block (id + etag) to `<slug>.json`.
func writeScheduledReportObject(dir string, o reconcile.Object) error {
	fields := map[string]any{}
	if len(o.Canonical) > 0 {
		if err := json.Unmarshal(o.Canonical, &fields); err != nil {
			return err
		}
	}
	server := map[string]any{"id": o.ServerID}
	if o.Etag != "" {
		server["etag"] = o.Etag
	}
	fields["_server"] = server
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}

func scheduledReportsCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		created, err := c.CreateScheduledReport(ctx, local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		if created.ID() == "" {
			// No server-assigned name echoed — a GET on "" would hit the collection.
			return reconcile.Object{}, fmt.Errorf("scheduled_reports: create %q returned no resource name", local.Slug)
		}
		full, err := c.GetScheduledReport(ctx, created.ID())
		if err != nil {
			return reconcile.Object{}, err
		}
		return scheduledReportObject(*full)
	}
}

func scheduledReportsUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		id := lastSegment(live.ServerID)
		// The PATCH body carries only the operator-authored keys, so the updateMask
		// must cover exactly those — a wider mask would clear an untouched field
		// (e.g. scopeInfo/userData) to its default. Derive it from what's present.
		mask, err := reportUpdateMask(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		// Optimistic concurrency needs a real etag; if List didn't populate one,
		// fetch the current report to read it before patching.
		etag := live.Etag
		if etag == "" {
			if cur, gerr := c.GetScheduledReport(ctx, id); gerr == nil {
				etag = cur.Etag
			}
		}
		body, err := withEtag(local.Canonical, etag)
		if err != nil {
			return reconcile.Object{}, err
		}
		if _, err := c.UpdateScheduledReport(ctx, id, body, mask); err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetScheduledReport(ctx, id)
		if err != nil {
			return reconcile.Object{}, err
		}
		return scheduledReportObject(*full)
	}
}

// reportWritableMaskKeys maps each operator-editable JSON key to its snake_case
// updateMask path.
var reportWritableMaskKeys = map[string]string{
	"displayName":     "display_name",
	"description":     "description",
	"userData":        "user_data",
	"dashboard":       "dashboard",
	"scopeInfo":       "scope_info",
	"cronDetails":     "cron_details",
	"deliveryDetails": "delivery_details",
	"format":          "format",
}

// reportUpdateMask returns the updateMask paths for exactly the writable keys
// present in the canonical body, so a PATCH never clears a field the operator
// didn't include.
func reportUpdateMask(canonical []byte) ([]string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, err
	}
	var mask []string
	for key, path := range reportWritableMaskKeys {
		if _, ok := m[key]; ok {
			mask = append(mask, path)
		}
	}
	sort.Strings(mask) // deterministic order
	return mask, nil
}

// withEtag injects etag into a JSON object body (omitted when empty).
func withEtag(canonical []byte, etag string) (json.RawMessage, error) {
	if etag == "" {
		return canonical, nil
	}
	var m map[string]any
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, err
	}
	m["etag"] = etag
	return json.Marshal(m)
}
