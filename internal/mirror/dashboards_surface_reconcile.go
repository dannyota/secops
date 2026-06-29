package mirror

// CREATE/UPDATE, chart reconcile helpers, validation, and small utilities for the
// dashboards surface. Types, Surface constructors, and the LIST/PULL side live in
// dashboards_surface.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

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
		addedNew := false
		allMatched := true
		wantIDs := map[string]bool{}
		for _, ch := range want.Definition.Charts {
			if ch.Server == nil || ch.Server.Chart == "" {
				if err := addDesiredChart(ctx, c, id, ch); err != nil {
					return reconcile.Object{}, fmt.Errorf("add chart %q: %w", ch.Title, err)
				}
				addedNew = true
				continue
			}
			wantIDs[ch.Server.Chart] = true
			lv, ok := liveByID[ch.Server.Chart]
			if !ok {
				warnf("dashboards: chart %q references id %s not present live; skipping (re-pull to refresh)",
					ch.Title, lastSegment(ch.Server.Chart))
				allMatched = false
				continue
			}
			if err := editDesiredChart(ctx, c, id, ch, lv); err != nil {
				return reconcile.Object{}, fmt.Errorf("edit chart %q: %w", ch.Title, err)
			}
			// Datasource edits and visualization/drilldown CLEARS can't be expressed
			// by editChart or the definition.charts PATCH, so they stay reported.
			if !rawEqual(ch.Datasource, lv.Datasource) {
				deferred = append(deferred, fmt.Sprintf("datasource of %q", ch.Title))
			}
			if (ch.Visualization == nil && lv.Visualization != nil) || (ch.DrillDownConfig == nil && lv.DrillDownConfig != nil) {
				deferred = append(deferred, fmt.Sprintf("visualization/drilldown clear of %q", ch.Title))
			}
		}

		// Removal is REPORTED, not applied: a live chart absent from the local
		// mirror may be an out-of-band UI edit, and push has no --prune gate for
		// sub-resources — deleting it on a (possibly stale) mirror would destroy
		// someone else's work. Remove a chart explicitly with `dashboards
		// remove-chart`.
		removals := 0
		for cid, lv := range liveByID {
			if !wantIDs[cid] {
				removals++
				deferred = append(deferred, fmt.Sprintf("removal of %q (delete it with `dashboards remove-chart`)", lv.Title))
			}
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

		// Chart LAYOUT / FILTERS / ORDER (reorder) reconcile through a
		// definition.charts PATCH — but ONLY when the desired and live chart SETS
		// are identical: every desired chart already resolves to a live id (no add
		// pending, none missing) AND there are no pending removals. The PATCH is a
		// wholesale charts-array replace, so running it on a differing set would
		// silently DROP a chart the operator did not explicitly remove. A membership
		// change defers the layout/order change to the next push (after the add/
		// remove lands and a re-pull mints/settles the ids).
		sameMembership := !addedNew && allMatched && removals == 0
		if sameMembership {
			if cfgs, ok := serverChartConfigs(want.Definition.Charts); ok && chartConfigsDiffer(want.Definition.Charts, have.Definition.Charts) {
				upd.Charts = cfgs
				changed = true
			}
		} else if chartConfigsDiffer(want.Definition.Charts, have.Definition.Charts) {
			deferred = append(deferred, "chart layout/order (deferred: the chart set changed — apply the add/remove, re-pull, then push)")
		}

		if changed {
			if _, err := c.UpdateDashboard(ctx, id, upd); err != nil {
				return reconcile.Object{}, err
			}
		}
		if len(deferred) > 0 {
			warnf("dashboards: %s reported, not applied by push (queries, chart content, and same-set layout/order WERE applied) — "+
				"remove charts with `dashboards remove-chart`; edit datasource / clear a visualization in the SecOps UI",
				strings.Join(deferred, "; "))
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

// serverChartConfigs builds the server-format definition.charts array (each
// element {dashboardChart: <resource name>, chartLayout, filtersIds}) in the
// desired order. ok is false if any chart lacks a resolved id (a just-added chart)
// — in that case the array would be incomplete and must NOT be sent.
func serverChartConfigs(charts []desiredChart) (cfgs []json.RawMessage, ok bool) {
	out := make([]json.RawMessage, 0, len(charts))
	for _, ch := range charts {
		if ch.Server == nil || ch.Server.Chart == "" {
			return nil, false
		}
		cfg := map[string]any{"dashboardChart": ch.Server.Chart}
		if len(ch.ChartLayout) > 0 {
			cfg["chartLayout"] = ch.ChartLayout
		}
		if len(ch.FiltersIds) > 0 {
			cfg["filtersIds"] = ch.FiltersIds
		}
		b, err := json.Marshal(cfg)
		if err != nil {
			return nil, false
		}
		out = append(out, b)
	}
	return out, true
}

// chartConfigsDiffer reports whether the desired charts differ from live in the
// dimensions a definition.charts PATCH controls: membership, ORDER, per-chart
// layout, and filtersIds (query/title/visualization are handled by editChart and
// are intentionally ignored here).
func chartConfigsDiffer(want, have []desiredChart) bool {
	if len(want) != len(have) {
		return true
	}
	for i := range want {
		w, h := want[i], have[i]
		wid, hid := "", ""
		if w.Server != nil {
			wid = w.Server.Chart
		}
		if h.Server != nil {
			hid = h.Server.Chart
		}
		if wid != hid { // membership or reorder
			return true
		}
		if !rawEqual(w.ChartLayout, h.ChartLayout) || !slices.Equal(w.FiltersIds, h.FiltersIds) {
			return true
		}
	}
	return false
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
	desired, _, err := buildDesiredDashboard(ctx, c, full.Raw, deref)
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

// validateDashboardObject statically checks a dashboard body the engine is about
// to create/update, catching the shapes the API rejects at apply time so the
// dry-run preview is a real safety check: a missing displayName, an
// empty new chart, a bad tileType token, or a non-object chartLayout. Structural
// only — it is not the server's full validation.
func validateDashboardObject(o reconcile.Object) error {
	d, err := parseDesired(o.Raw)
	if err != nil {
		return fmt.Errorf("not valid dashboard JSON: %w", err)
	}
	if strings.TrimSpace(d.DisplayName) == "" {
		return fmt.Errorf("displayName is required")
	}
	for i, ch := range d.Definition.Charts {
		label := ch.Title
		if label == "" {
			label = fmt.Sprintf("chart %d", i+1)
		}
		// A brand-new chart (no server id) with neither a title nor a query is an
		// empty chart the API rejects; an existing/reference chart may legitimately
		// carry only a layout.
		isNew := ch.Server == nil || ch.Server.Chart == ""
		if isNew && strings.TrimSpace(ch.Title) == "" && strings.TrimSpace(ch.Query) == "" {
			return fmt.Errorf("%s: a new chart needs a title or a query", label)
		}
		if ch.TileType != "" && ch.TileType != chronicle.TileTypeVisualization && ch.TileType != chronicle.TileTypeButton {
			return fmt.Errorf("%s: invalid tileType %q (want %s | %s)", label, ch.TileType, chronicle.TileTypeVisualization, chronicle.TileTypeButton)
		}
		if len(ch.ChartLayout) > 0 && !isJSONObject(ch.ChartLayout) {
			return fmt.Errorf("%s: chartLayout must be a JSON object", label)
		}
	}
	return nil
}

// isJSONObject reports whether raw decodes to a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	var m map[string]any
	return json.Unmarshal(raw, &m) == nil
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
