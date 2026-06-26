package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
)

// Deep-copy a native dashboard, client-side. The server's :duplicate verb
// (DuplicateDashboard) already produces an independent copy — its own freshly
// minted charts and queries, not references to the source's — and is the default
// path. DeepCopyDashboard is the fallback: it reads the source and recreates each
// chart (and its query) fresh on a new dashboard via AddChart, without relying on
// the server verb (useful when a server-side copy is unavailable, or to copy with
// custom per-chart handling). Both yield a fully independent dashboard.

// copyChartRef is one entry of a dashboard's definition.charts: a reference to a
// chart plus its grid placement.
type copyChartRef struct {
	DashboardChart string          `json:"dashboardChart"`
	ChartLayout    json.RawMessage `json:"chartLayout"`
}

// copyChartBody is the slice of a GetChart response DeepCopyDashboard recreates
// from. ChartDatasource is kept raw so every field it carries is preserved on the
// copy (only the embedded dashboardQuery reference is swapped for a fresh query).
type copyChartBody struct {
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description"`
	TileType        string          `json:"tileType"`
	Visualization   json.RawMessage `json:"visualization"`
	DrillDownConfig json.RawMessage `json:"drillDownConfig"`
	ChartDatasource json.RawMessage `json:"chartDatasource"`
}

// splitDatasource returns the embedded dashboardQuery resource reference and the
// datasource with that reference removed — every other field (dataSources and any
// config this SDK does not model) preserved — so the recreated chart keeps its
// full datasource yet gets a fresh, owned query rather than re-referencing the
// source's. A datasource with no dashboardQuery passes through unchanged.
func splitDatasource(raw json.RawMessage) (queryRef string, rest json.RawMessage, err error) {
	if len(raw) == 0 {
		return "", nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, fmt.Errorf("decode chartDatasource: %w", err)
	}
	if q, ok := m["dashboardQuery"]; ok {
		_ = json.Unmarshal(q, &queryRef) // a string ref; leave empty if not a string
		delete(m, "dashboardQuery")
	}
	rest, err = json.Marshal(m)
	return queryRef, rest, err
}

// copyQueryBody is the slice of a GetQuery response the deep-copy needs: the
// UDM query text and its input interval, replayed inline into AddChart so the
// server mints a fresh query resource for the new chart.
type copyQueryBody struct {
	Query string          `json:"query"`
	Input json.RawMessage `json:"input"`
}

// copyDefinition is the subset of a dashboard the deep-copy reads (parsed from
// NativeDashboard.Raw, which has no typed Definition/Description fields).
type copyDefinition struct {
	Description string `json:"description"`
	Definition  struct {
		Filters []json.RawMessage `json:"filters"`
		Charts  []json.RawMessage `json:"charts"`
	} `json:"definition"`
}

// DeepCopyDashboard creates an independent CUSTOM copy of a dashboard: a new
// dashboard carrying the source's filters, with each of the source's charts
// recreated fresh (its own chart and its own query) on the copy. accessType is
// DashboardPublic or DashboardPrivate; an empty description inherits the
// source's. Returns the new dashboard (full view).
//
// This is a client-side alternative to the server :duplicate verb
// (DuplicateDashboard); both produce an independent copy. Prefer the server verb
// (one call); use this when a server-side copy is unavailable.
func (c *Client) DeepCopyDashboard(ctx context.Context, srcID, displayName, accessType, description string) (*NativeDashboard, error) {
	src, err := c.GetDashboard(ctx, srcID, true)
	if err != nil {
		return nil, err
	}
	var def copyDefinition
	if err := json.Unmarshal(src.Raw, &def); err != nil {
		return nil, fmt.Errorf("chronicle: deep-copy parse source definition: %w", err)
	}
	if description == "" {
		description = def.Description
	}

	created, err := c.CreateDashboard(ctx, displayName, description, accessType, def.Definition.Filters, nil)
	if err != nil {
		return nil, err
	}
	newID := resourceID(created.Name)

	// Recreate each chart on the new dashboard. On any failure mid-loop, roll back
	// by deleting the partially-built dashboard, so a failed copy never leaves an
	// orphan live on the instance.
	if err := c.copyCharts(ctx, newID, def.Definition.Charts); err != nil {
		if delErr := c.DeleteDashboard(ctx, newID); delErr != nil {
			return nil, fmt.Errorf("%w; rollback failed, orphan dashboard %s left on the instance: %v", err, newID, delErr) //nolint:errorlint // rollback error is annotation only
		}
		return nil, err
	}
	return c.GetDashboard(ctx, newID, true)
}

// copyCharts recreates each source chart on dashboard newID. The source chart
// bodies are fetched in ONE dashboardCharts:batchGet (rather than a GetChart per
// chart); each chart's query is then read and the chart recreated. Any error
// aborts and is returned to the caller (which rolls the new dashboard back).
func (c *Client) copyCharts(ctx context.Context, newID string, charts []json.RawMessage) error {
	refs := make([]copyChartRef, 0, len(charts))
	ids := make([]string, 0, len(charts))
	for i, raw := range charts {
		var ref copyChartRef
		if err := json.Unmarshal(raw, &ref); err != nil {
			return fmt.Errorf("chronicle: deep-copy chart[%d] reference: %w", i, err)
		}
		if ref.DashboardChart != "" {
			refs = append(refs, ref)
			ids = append(ids, ref.DashboardChart)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	bodies, err := c.BatchGetCharts(ctx, ids)
	if err != nil {
		return fmt.Errorf("chronicle: deep-copy batch-get charts: %w", err)
	}
	byID := make(map[string]json.RawMessage, len(bodies))
	for _, b := range bodies {
		var nm struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &nm) == nil && nm.Name != "" {
			byID[resourceID(nm.Name)] = b
		}
	}
	for i, ref := range refs {
		chRaw, ok := byID[resourceID(ref.DashboardChart)]
		if !ok {
			return fmt.Errorf("chronicle: deep-copy chart[%d] (%s): not returned by batchGet", i, resourceID(ref.DashboardChart))
		}
		in, err := c.copyChartInput(ctx, ref, chRaw)
		if err != nil {
			return fmt.Errorf("chronicle: deep-copy chart[%d] (%s): %w", i, resourceID(ref.DashboardChart), err)
		}
		if _, err := c.AddChart(ctx, newID, in); err != nil {
			return fmt.Errorf("chronicle: deep-copy AddChart[%d] (%q): %w", i, in.DisplayName, err)
		}
	}
	return nil
}

// copyChartInput builds the AddChartInput that recreates a chart fresh from its
// already-fetched body (chRaw, from the batch get) and its query (read here — the
// query has no batch endpoint): the datasource keeps only dataSources (dropping
// the old query reference) and the query text/interval are replayed inline so the
// server creates a new query owned by the new chart.
func (c *Client) copyChartInput(ctx context.Context, ref copyChartRef, chRaw json.RawMessage) (AddChartInput, error) {
	var ch copyChartBody
	if err := json.Unmarshal(chRaw, &ch); err != nil {
		return AddChartInput{}, fmt.Errorf("decode chart: %w", err)
	}
	queryRef, ds, err := splitDatasource(ch.ChartDatasource)
	if err != nil {
		return AddChartInput{}, err
	}
	var q *copyQueryBody
	if queryRef != "" {
		qRaw, err := c.GetQuery(ctx, queryRef)
		if err != nil {
			return AddChartInput{}, fmt.Errorf("get query: %w", err)
		}
		q = &copyQueryBody{}
		if err := json.Unmarshal(qRaw, q); err != nil {
			return AddChartInput{}, fmt.Errorf("decode query: %w", err)
		}
	}
	return assembleChartInput(ref, ch, ds, q)
}

// assembleChartInput builds the AddChartInput for a recreated chart from the
// already-fetched source chart, its datasource (with the old query reference
// already stripped — see splitDatasource), and the optional query. Pure: no API
// calls. The query is replayed inline so the server mints a fresh, owned query.
//
// AddChart only attaches a query when BOTH the query text and its input interval
// are present, so a source query with text but no interval would be silently
// dropped — that is rejected loudly here instead, matching `dashboards add-chart`.
func assembleChartInput(ref copyChartRef, ch copyChartBody, ds json.RawMessage, q *copyQueryBody) (AddChartInput, error) {
	in := AddChartInput{
		DisplayName:     ch.DisplayName,
		Description:     ch.Description,
		TileType:        ch.TileType,
		Visualization:   ch.Visualization,
		DrillDownConfig: ch.DrillDownConfig,
		ChartLayout:     ref.ChartLayout,
		ChartDatasource: ds,
	}
	if q != nil && q.Query != "" {
		if len(q.Input) == 0 {
			return AddChartInput{}, fmt.Errorf("source chart %q has a query but no input interval; cannot copy it faithfully", ch.DisplayName)
		}
		in.Query = q.Query
		in.Interval = q.Input
	}
	return in, nil
}
