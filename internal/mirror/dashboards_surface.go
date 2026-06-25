package mirror

// Types, Surface constructors, and LIST/PULL side for the dashboards surface.
// CREATE/UPDATE, chart reconcile helpers, validation, and small utilities live in
// dashboards_surface_reconcile.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// dashboards (native Chronicle dashboards) as code, on the reconcile engine.
// Only CUSTOM dashboards are managed — CURATED dashboards are Google-managed with
// no create/update path, so they are excluded from the surface entirely.
//
// A dashboard is a 3-resource graph: the dashboard's definition.charts[] only
// REFERENCES each chart by resource name, the chart references its query, and the
// query holds the YARA-L. To make a dashboard round-trip as code, `pull` follows
// those references and writes each chart INLINE under definition.charts —
// title / tileType / layout / filters / datasource / visualization plus the
// YARA-L `query` + `interval` — with a per-chart `_server` block (the chart and
// query resource names, used to match on push and stripped from the diff basis).
//
// `pull dashboards` is REFERENCE-ONLY by default — each chart is captured as its
// layout + filters + a `_server.chart` id, with no extra API calls — so the
// everyday pull stays cheap and `drift` stays deterministic. `pull dashboards
// --with-charts` dereferences every chart into its inline query/visualization
// (heavier: a GetChart + GetQuery per chart, which can hit the per-minute quota
// on a large instance — charts that 404/429 stay as references). `push` adapts to
// whichever shape is on disk, so an inline mirror reconciles its queries while a
// reference-only mirror just reconciles dashboard-level fields.
//
// On disk each dashboard is one `<slug>.json`: the canonical config plus a
// reserved top-level `_server` id. `push` reconciles:
//   - dashboard-level fields (displayName/description/filters) via PATCH;
//   - a new chart (no `_server`) via :addChart (creates chart + query);
//   - a changed query/title/visualization/drilldown via :editChart (etag-guarded);
//   - chart LAYOUT, filtersIds, and REORDER via a definition.charts PATCH — but
//     ONLY when the desired and live chart SETS are identical (the PATCH replaces
//     the charts array wholesale, so a differing set could drop a chart); a
//     membership change defers the layout/order to the next push.
//
// Chart REMOVAL is REPORTED, not applied (push has no --prune gate for
// sub-resources; deleting a chart absent from a stale mirror would destroy an
// out-of-band edit) — remove charts explicitly with `dashboards remove-chart`.
// Datasource edits and visualization/drilldown CLEARS are likewise reported, not
// applied (neither editChart nor the PATCH can express them) — make those in the UI.

// dashboardExtraStrip are keys dropped from the diff basis at ANY depth.
// `dashboardUserData` is per-viewer state. `_server` is the reserved identity
// block this surface injects both at the dashboard root and on every inline chart
// (chart+query resource names) — stripping it at depth keeps server ids out of
// the diff so they never churn. (createUserId/updateUserId are stripped globally
// by the engine via reconcile.actorKeys.)
var dashboardExtraStrip = []string{"dashboardUserData", "_server"}

// chartServer is the per-chart reserved identity: the chart and query resource
// names. Written on pull, used to match desired↔live on push, stripped from the
// diff basis.
type chartServer struct {
	Chart string `json:"chart,omitempty"`
	Query string `json:"query,omitempty"`
}

// desiredChart is the inline, reference-free chart shape stored under
// definition.charts. It is what the operator edits and what reconciles; the YARA-L
// lives in `query` (not behind a reference) so the dashboard round-trips intact.
type desiredChart struct {
	Title           string          `json:"title"`
	TileType        string          `json:"tileType,omitempty"`
	ChartLayout     json.RawMessage `json:"chartLayout,omitempty"`
	FiltersIds      []string        `json:"filtersIds,omitempty"`
	Datasource      json.RawMessage `json:"datasource,omitempty"`
	Visualization   json.RawMessage `json:"visualization,omitempty"`
	DrillDownConfig json.RawMessage `json:"drillDownConfig,omitempty"`
	Query           string          `json:"query,omitempty"`
	Interval        json.RawMessage `json:"interval,omitempty"`
	Server          *chartServer    `json:"_server,omitempty"`
}

// desiredDashboard is the on-disk dashboard shape (the server resource `name` is
// kept for identity extraction and stripped from the canonical).
type desiredDashboard struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Access      string `json:"access,omitempty"`
	Type        string `json:"type,omitempty"`
	Definition  struct {
		Filters []json.RawMessage `json:"filters,omitempty"`
		Charts  []desiredChart    `json:"charts,omitempty"`
	} `json:"definition"`
}

// dashboardsSurface is the default (reference-only) dashboards surface used by
// the registry. Every chart is kept as a reference.
func dashboardsSurface(c *chronicle.Client) reconcile.Surface {
	return dashboardsSurfaceWith(c, func(string) bool { return false })
}

// DashboardsSurfaceDeref is the `pull --with-charts` variant: every chart is
// dereferenced into its inline query.
func DashboardsSurfaceDeref(c *chronicle.Client) reconcile.Surface {
	return dashboardsSurfaceWith(c, func(string) bool { return true })
}

// DashboardsSurfaceForMirror is the push/drift variant: a live dashboard is
// dereferenced iff its on-disk mirror file is inline, so each dashboard is
// compared in its OWN shape (a mixed inline/reference mirror never phantom-diffs).
func DashboardsSurfaceForMirror(c *chronicle.Client, dir string) reconcile.Surface {
	inline := dashboardsInlineServerIDs(dir)
	return dashboardsSurfaceWith(c, func(name string) bool { return inline[name] })
}

// derefFor decides, per live dashboard (by full resource name), whether to
// dereference its charts into inline queries.
func dashboardsSurfaceWith(c *chronicle.Client, derefFor func(name string) bool) reconcile.Surface {
	return reconcile.Surface{
		Name:    "dashboards",
		Dir:     DirDashboards,
		Product: reconcile.ProductSIEM,
		// No dashboard-level etag round-trip; delete loses a whole dashboard
		// (high-blast) → not prune-eligible.
		Caps: reconcile.Capabilities{NoEtag: true},

		List:     dashboardsList(c, derefFor),
		LoadDir:  loadDashboards,
		Write:    writeDashboardObject,
		Create:   dashboardsCreate(c),
		Update:   dashboardsUpdate(c),
		Validate: validateDashboardObject,
		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteDashboard(ctx, lastSegment(live.ServerID))
		},
	}
}

// dashboardsInlineServerIDs maps the full resource name of each on-disk dashboard
// whose mirror file is inline (query-bearing) to true. Keyed by server id (not
// slug) so duplicate display names don't collide.
func dashboardsInlineServerIDs(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		if id, _ := serverBlock(b); id != "" && dashboardWantsInline(b) {
			out[id] = true
		}
	}
	return out
}

// dashboardsList reads every CUSTOM dashboard in FULL view. derefFor decides, per
// dashboard, whether to follow each chart reference to its inline query; otherwise
// charts are kept as references. CURATED dashboards are skipped.
func dashboardsList(c *chronicle.Client, derefFor func(name string) bool) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		list, err := c.ListNativeDashboards(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		degradedCharts, degradedDashboards := 0, 0
		for _, d := range list {
			if d.Type != "CUSTOM" {
				continue // CURATED dashboards are read-only/unmanaged
			}
			full, gerr := c.GetDashboard(ctx, lastSegment(d.Name), true)
			if gerr != nil {
				warnf("dashboards: get %s full: %v", d.DisplayName, gerr)
				res.Incomplete = true
				continue
			}
			desired, degraded, derr := buildDesiredDashboard(ctx, c, full.Raw, derefFor(d.Name))
			if derr != nil {
				warnf("dashboards: deref %s: %v", d.DisplayName, derr)
				res.Incomplete = true
				continue
			}
			if degraded > 0 {
				degradedCharts += degraded
				degradedDashboards++
			}
			o, berr := buildDashboardObject(desired)
			if berr != nil {
				warnf("dashboards: build %s: %v", d.DisplayName, berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		// Surface degraded charts loudly: a chart that 404/429'd on the per-chart
		// deref was kept as a reference (no query captured), so the mirror is
		// partially reference-only. Without this the partial state is silent until a
		// later drift. Re-pull to capture the charts that were transiently
		// unavailable.
		if degradedCharts > 0 {
			warnf("dashboards: %d chart(s) across %d dashboard(s) degraded to a reference (404/429 on deref) — "+
				"those charts' queries are NOT in the mirror; re-run `pull dashboards --with-charts` to capture them",
				degradedCharts, degradedDashboards)
		}
		return res, nil
	}
}

// buildDesiredDashboard converts a live FULL-view dashboard into the on-disk
// desired shape. When deref is set, each chart reference is followed to its
// dashboardChart + dashboardQuery so the query text is captured locally;
// otherwise each chart is kept as a reference (layout + filters + _server.chart).
func buildDesiredDashboard(ctx context.Context, c *chronicle.Client, raw json.RawMessage, deref bool) (json.RawMessage, int, error) {
	degraded := 0
	var live struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Access      string `json:"access"`
		Type        string `json:"type"`
		Definition  struct {
			Filters []json.RawMessage `json:"filters"`
			Charts  []struct {
				DashboardChart string          `json:"dashboardChart"`
				ChartLayout    json.RawMessage `json:"chartLayout"`
				FiltersIds     []string        `json:"filtersIds"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(raw, &live); err != nil {
		return nil, 0, err
	}
	var d desiredDashboard
	d.Name, d.DisplayName, d.Description = live.Name, live.DisplayName, live.Description
	d.Access, d.Type = live.Access, live.Type
	d.Definition.Filters = live.Definition.Filters

	// Deref each chart → its query. A chart that won't resolve standalone (some
	// charts 404 on the per-chart GET; a burst can also 429) degrades to a
	// REFERENCE — layout + filters + the _server.chart id, no inline body — rather
	// than losing the whole dashboard. A ref-only chart round-trips as a clean
	// no-op on push (it matches live by id with nothing to edit). Re-pull to pick
	// up a chart that was only transiently unavailable.
	for _, cc := range live.Definition.Charts {
		dc := desiredChart{ChartLayout: cc.ChartLayout, FiltersIds: cc.FiltersIds}
		if cc.DashboardChart == "" {
			d.Definition.Charts = append(d.Definition.Charts, dc)
			continue
		}
		dc.Server = &chartServer{Chart: cc.DashboardChart}
		if !deref {
			// reference-only: keep the chart id + layout, no per-chart fetch.
			d.Definition.Charts = append(d.Definition.Charts, dc)
			continue
		}
		chartRaw, err := c.GetChart(ctx, cc.DashboardChart)
		if err != nil {
			warnf("dashboards: %q chart %s not dereferenced (kept as a reference): %v",
				live.DisplayName, lastSegment(cc.DashboardChart), err)
			degraded++
			d.Definition.Charts = append(d.Definition.Charts, dc)
			continue
		}
		var ch struct {
			DisplayName     string          `json:"displayName"`
			TileType        string          `json:"tileType"`
			Visualization   json.RawMessage `json:"visualization"`
			DrillDownConfig json.RawMessage `json:"drillDownConfig"`
			ChartDatasource struct {
				DashboardQuery string   `json:"dashboardQuery"`
				DataSources    []string `json:"dataSources"`
			} `json:"chartDatasource"`
		}
		if err := json.Unmarshal(chartRaw, &ch); err != nil {
			return nil, degraded, err
		}
		dc.Title, dc.TileType = ch.DisplayName, ch.TileType
		dc.Visualization, dc.DrillDownConfig = ch.Visualization, ch.DrillDownConfig
		if len(ch.ChartDatasource.DataSources) > 0 {
			ds, _ := json.Marshal(map[string]any{"dataSources": ch.ChartDatasource.DataSources})
			dc.Datasource = ds
		}
		if qref := ch.ChartDatasource.DashboardQuery; qref != "" {
			dc.Server.Query = qref
			qRaw, qerr := c.GetQuery(ctx, qref)
			if qerr != nil {
				warnf("dashboards: %q chart %q query not dereferenced: %v", live.DisplayName, dc.Title, qerr)
				degraded++
			} else {
				var q struct {
					Query string          `json:"query"`
					Input json.RawMessage `json:"input"`
				}
				if err := json.Unmarshal(qRaw, &q); err != nil {
					return nil, degraded, err
				}
				dc.Query, dc.Interval = q.Query, q.Input
			}
		}
		d.Definition.Charts = append(d.Definition.Charts, dc)
	}
	out, err := json.Marshal(d)
	return out, degraded, err
}

// dashboardCanonical strips the root resource name (identity, carried in
// ServerID) and then canonicalizes (dashboardExtraStrip drops `_server` and
// `dashboardUserData` at any depth).
func dashboardCanonical(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "name")
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(b, dashboardExtraStrip...)
}

// buildDashboardObject canonicalizes the inline desired body and extracts identity.
// Raw keeps the full desired body (incl. per-chart `_server`) for the writer and
// for the Update closure's chart matching.
func buildDashboardObject(raw json.RawMessage) (reconcile.Object, error) {
	canon, err := dashboardCanonical(raw)
	if err != nil {
		return reconcile.Object{}, err
	}
	id := jsonField(raw, "name")
	if id == "" {
		return reconcile.Object{}, fmt.Errorf("dashboard has no resource name")
	}
	slug := jsonField(raw, "displayName")
	if slug == "" {
		slug = lastSegment(id)
	}
	return reconcile.Object{Slug: Slugify(slug), ServerID: id, Canonical: canon, Raw: raw}, nil
}

func loadDashboards(dir string) ([]reconcile.Object, error) {
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
		id, _ := serverBlock(b)
		canon, cerr := dashboardCanonical(b)
		if cerr != nil {
			return nil, fmt.Errorf("dashboards: canonicalize %s: %w", e.Name(), cerr)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Canonical: canon,
			Raw:       b, // full body incl. per-chart _server, for Update matching
		})
	}
	return objs, nil
}

// writeDashboardObject renders the full desired body (charts inline, per-chart
// `_server` preserved) minus the root `name`, plus a top-level `_server` id, to
// `<slug>.json`.
func writeDashboardObject(dir string, o reconcile.Object) error {
	fields := map[string]any{}
	if len(o.Raw) > 0 {
		if err := json.Unmarshal(o.Raw, &fields); err != nil {
			return err
		}
	}
	delete(fields, "name")
	fields["_server"] = map[string]any{"id": o.ServerID}
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}
