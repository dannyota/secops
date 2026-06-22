package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
//   - a changed query/title/visualization/drilldown via :editChart (etag-guarded).
// Chart LAYOUT/filters/datasource edits, reordering, and chart REMOVAL are not
// applied by push — they are reported so the change is never silently dropped;
// edit layout in the UI and remove charts with `dashboards remove-chart`.

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

		List:    dashboardsList(c, derefFor),
		LoadDir: loadDashboards,
		Write:   writeDashboardObject,
		Create:  dashboardsCreate(c),
		Update:  dashboardsUpdate(c),
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
			desired, derr := buildDesiredDashboard(ctx, c, full.Raw, derefFor(d.Name))
			if derr != nil {
				warnf("dashboards: deref %s: %v", d.DisplayName, derr)
				res.Incomplete = true
				continue
			}
			o, berr := buildDashboardObject(desired)
			if berr != nil {
				warnf("dashboards: build %s: %v", d.DisplayName, berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// buildDesiredDashboard converts a live FULL-view dashboard into the on-disk
// desired shape. When deref is set, each chart reference is followed to its
// dashboardChart + dashboardQuery so the query text is captured locally;
// otherwise each chart is kept as a reference (layout + filters + _server.chart).
func buildDesiredDashboard(ctx context.Context, c *chronicle.Client, raw json.RawMessage, deref bool) (json.RawMessage, error) {
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
		return nil, err
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
			return nil, err
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
			} else {
				var q struct {
					Query string          `json:"query"`
					Input json.RawMessage `json:"input"`
				}
				if err := json.Unmarshal(qRaw, &q); err != nil {
					return nil, err
				}
				dc.Query, dc.Interval = q.Query, q.Input
			}
		}
		d.Definition.Charts = append(d.Definition.Charts, dc)
	}
	return json.Marshal(d)
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

func dashboardsCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		want, err := parseDesired(local.Raw)
		if err != nil {
			return reconcile.Object{}, err
		}
		created, err := c.CreateDashboard(ctx, want.DisplayName, want.Description, want.Access,
			want.Definition.Filters, nil)
		if err != nil {
			return reconcile.Object{}, err
		}
		id := lastSegment(created.Name)
		for _, ch := range want.Definition.Charts {
			if err := addDesiredChart(ctx, c, id, ch); err != nil {
				return reconcile.Object{}, fmt.Errorf("add chart %q: %w", ch.Title, err)
			}
		}
		return refreshDashboard(ctx, c, id, dashboardWantsInline(local.Raw))
	}
}

func dashboardsUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		want, err := parseDesired(local.Raw)
		if err != nil {
			return reconcile.Object{}, err
		}
		have, err := parseDesired(live.Raw)
		if err != nil {
			return reconcile.Object{}, err
		}
		if want.Access != "" && have.Access != "" && want.Access != have.Access {
			return reconcile.Object{}, fmt.Errorf(
				"dashboards: access of %q changed (%s -> %s); access is immutable after create — recreate the dashboard to change it",
				lastSegment(live.ServerID), have.Access, want.Access)
		}
		id := lastSegment(live.ServerID)

		liveByID := map[string]desiredChart{}
		for _, ch := range have.Definition.Charts {
			if ch.Server != nil && ch.Server.Chart != "" {
				liveByID[ch.Server.Chart] = ch
			}
		}

		var deferred []string
		wantIDs := map[string]bool{}
		for _, ch := range want.Definition.Charts {
			if ch.Server == nil || ch.Server.Chart == "" {
				if err := addDesiredChart(ctx, c, id, ch); err != nil {
					return reconcile.Object{}, fmt.Errorf("add chart %q: %w", ch.Title, err)
				}
				continue
			}
			wantIDs[ch.Server.Chart] = true
			lv, ok := liveByID[ch.Server.Chart]
			if !ok {
				warnf("dashboards: chart %q references id %s not present live; skipping (re-pull to refresh)",
					ch.Title, lastSegment(ch.Server.Chart))
				continue
			}
			if err := editDesiredChart(ctx, c, id, ch, lv); err != nil {
				return reconcile.Object{}, fmt.Errorf("edit chart %q: %w", ch.Title, err)
			}
			if !rawEqual(ch.ChartLayout, lv.ChartLayout) || !slices.Equal(ch.FiltersIds, lv.FiltersIds) {
				deferred = append(deferred, fmt.Sprintf("layout/filters of %q", ch.Title))
			}
			if !rawEqual(ch.Datasource, lv.Datasource) {
				deferred = append(deferred, fmt.Sprintf("datasource of %q", ch.Title))
			}
			// A field cleared to nil isn't applied by editChart (it would be an
			// empty edit) — report it rather than silently dropping the clear.
			if (ch.Visualization == nil && lv.Visualization != nil) || (ch.DrillDownConfig == nil && lv.DrillDownConfig != nil) {
				deferred = append(deferred, fmt.Sprintf("visualization/drilldown clear of %q", ch.Title))
			}
		}
		for cid, lv := range liveByID {
			if !wantIDs[cid] {
				deferred = append(deferred, fmt.Sprintf("removal of %q", lv.Title))
			}
		}
		if len(deferred) > 0 {
			warnf("dashboards: %s not reconciled by push (queries and chart content WERE applied) — "+
				"edit layout/order/datasource in the SecOps UI, remove charts with `dashboards remove-chart`",
				strings.Join(deferred, "; "))
		}

		upd := chronicle.DashboardUpdate{}
		changed := false
		if want.DisplayName != have.DisplayName {
			upd.DisplayName = &want.DisplayName
			changed = true
		}
		if want.Description != have.Description {
			upd.Description = &want.Description
			changed = true
		}
		if !rawSliceEqual(want.Definition.Filters, have.Definition.Filters) {
			upd.Filters = nonNilRaw(want.Definition.Filters)
			changed = true
		}
		if changed {
			if _, err := c.UpdateDashboard(ctx, id, upd); err != nil {
				return reconcile.Object{}, err
			}
		}
		return refreshDashboard(ctx, c, id, dashboardWantsInline(local.Raw))
	}
}

// --- chart reconcile helpers ------------------------------------------------

// addDesiredChart creates a chart (and its query) on a dashboard via :addChart.
// chartLayout is required by the API, so a missing one gets a default.
func addDesiredChart(ctx context.Context, c *chronicle.Client, dashID string, ch desiredChart) error {
	layout := ch.ChartLayout
	if len(layout) == 0 {
		// Native dashboards use a 96-column grid; default a chart with no layout
		// to full width and the most common row height.
		layout = json.RawMessage(`{"startX":0,"spanX":96,"startY":0,"spanY":16}`)
	}
	// The server attaches a query only when both query and interval are present,
	// so a query authored without an interval would silently be dropped — default
	// the interval rather than lose the query.
	interval := ch.Interval
	if ch.Query != "" && len(interval) == 0 {
		interval = json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`)
	}
	_, err := c.AddChart(ctx, dashID, chronicle.AddChartInput{
		DisplayName:     ch.Title,
		TileType:        ch.TileType,
		ChartLayout:     layout,
		ChartDatasource: ch.Datasource,
		Visualization:   ch.Visualization,
		DrillDownConfig: ch.DrillDownConfig,
		Query:           ch.Query,
		Interval:        interval,
	})
	return err
}

// editDesiredChart applies the safe, reconcilable chart edits: the YARA-L
// query/interval (dashboardQuery) and the chart's display name / visualization /
// drilldown (dashboardChart). It does NOT touch chartDatasource (it carries the
// query reference — editing it risks breaking the link) or layout. Etags are
// fetched fresh right before the edit.
func editDesiredChart(ctx context.Context, c *chronicle.Client, dashID string, want, live desiredChart) error {
	in := chronicle.EditChartInput{}

	if want.Query != live.Query || !rawEqual(want.Interval, live.Interval) {
		if want.Server == nil || want.Server.Query == "" {
			warnf("dashboards: chart %q has no existing query to edit; add a query via the UI or recreate the chart", want.Title)
		} else {
			etag := ""
			if qRaw, err := c.GetQuery(ctx, want.Server.Query); err == nil {
				etag = jsonField(qRaw, "etag")
			}
			body := map[string]any{"name": want.Server.Query, "query": want.Query, "etag": etag}
			if want.Interval != nil {
				body["input"] = want.Interval
			}
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			in.DashboardQuery = b
		}
	}

	// Collect the chart-resource fields that have a value to SET. A field cleared
	// to nil (want nil, live present) is NOT sent here — clearing via editChart
	// would otherwise yield an empty edit mask (an error); it is reported as a
	// deferred change by the caller instead.
	fields := map[string]any{}
	if want.Title != live.Title {
		fields["displayName"] = want.Title
	}
	if want.Visualization != nil && !rawEqual(want.Visualization, live.Visualization) {
		fields["visualization"] = want.Visualization
	}
	if want.DrillDownConfig != nil && !rawEqual(want.DrillDownConfig, live.DrillDownConfig) {
		fields["drillDownConfig"] = want.DrillDownConfig
	}
	if len(fields) > 0 {
		etag := ""
		if cRaw, err := c.GetChart(ctx, want.Server.Chart); err == nil {
			etag = jsonField(cRaw, "etag")
		}
		fields["name"], fields["etag"] = want.Server.Chart, etag
		b, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		in.DashboardChart = b
	}

	if len(in.DashboardQuery) == 0 && len(in.DashboardChart) == 0 {
		return nil
	}
	_, err := c.EditChart(ctx, dashID, in)
	return err
}

// refreshDashboard re-fetches a dashboard in FULL view and rebuilds the engine
// object (so the on-disk mirror matches live after a mutation). deref matches the
// shape the operator is keeping — inline if their mirror carries chart queries,
// reference-only otherwise — so a push never silently up- or down-grades the shape.
func refreshDashboard(ctx context.Context, c *chronicle.Client, id string, deref bool) (reconcile.Object, error) {
	full, err := c.GetDashboard(ctx, id, true)
	if err != nil {
		return reconcile.Object{}, err
	}
	desired, err := buildDesiredDashboard(ctx, c, full.Raw, deref)
	if err != nil {
		return reconcile.Object{}, err
	}
	return buildDashboardObject(desired)
}

// dashboardWantsInline reports whether an on-disk dashboard body is the inline
// (query-bearing) shape, so a push refresh preserves it. A reference-only mirror
// has no chart titles/queries.
func dashboardWantsInline(raw json.RawMessage) bool {
	d, err := parseDesired(raw)
	if err != nil {
		return false
	}
	for _, ch := range d.Definition.Charts {
		if ch.Title != "" || ch.Query != "" {
			return true
		}
	}
	return false
}

// --- small helpers ----------------------------------------------------------

func parseDesired(raw json.RawMessage) (desiredDashboard, error) {
	var d desiredDashboard
	err := json.Unmarshal(raw, &d)
	return d, err
}

// rawEqual reports whether two raw JSON values are semantically equal (key order
// normalized). Two empty values are equal.
func rawEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return bytes.Equal(a, b)
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(ab, bb)
}

func rawSliceEqual(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !rawEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// nonNilRaw guarantees a non-nil slice so an absent filters list is sent as an
// explicit empty replacement (a true wholesale replace) rather than "unchanged".
func nonNilRaw(s []json.RawMessage) []json.RawMessage {
	if s == nil {
		return []json.RawMessage{}
	}
	return s
}
