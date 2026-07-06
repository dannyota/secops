package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Dashboard chart and query write surface (add/get/edit/remove chart,
// execute/get query). Complements dashboards_write.go (dashboard CRUD) and
// dashboards.go (read-only list/export).

// AddChart adds a chart to a dashboard. chartLayout is required (freeform JSON).
// tileType defaults to TileTypeVisualization when empty. chartDatasource,
// visualization, and drillDownConfig are optional freeform JSON blobs. If both
// query and interval are non-nil, a dashboardQuery is attached. The endpoint is
// POST {instance}/nativeDashboards/<id>:addChart.
//
// DEVIATION: the wrapper's freeform **kwargs splat is dropped; pass any extra
// chart fields inside the chart blobs instead.
type AddChartInput struct {
	DisplayName     string
	Description     string
	TileType        string          // defaults to TileTypeVisualization
	ChartLayout     json.RawMessage // required
	ChartDatasource json.RawMessage // optional
	Visualization   json.RawMessage // optional
	DrillDownConfig json.RawMessage // optional
	Query           string          // optional UDM query; paired with Interval
	Interval        json.RawMessage // optional input interval; paired with Query
}

// dashboardChartBody is the "dashboardChart" sub-object of an addChart request.
type dashboardChartBody struct {
	DisplayName     string          `json:"displayName"`
	TileType        string          `json:"tileType"`
	Description     string          `json:"description,omitempty"`
	ChartDatasource json.RawMessage `json:"chartDatasource,omitempty"`
	Visualization   json.RawMessage `json:"visualization,omitempty"`
	DrillDownConfig json.RawMessage `json:"drillDownConfig,omitempty"`
}

// AddChart implements POST nativeDashboards/<id>:addChart.
func (c *Client) AddChart(ctx context.Context, dashboardID string, in AddChartInput) (json.RawMessage, error) {
	tile := in.TileType
	if tile == "" {
		tile = TileTypeVisualization
	}
	body := struct {
		DashboardChart dashboardChartBody `json:"dashboardChart"`
		ChartLayout    json.RawMessage    `json:"chartLayout,omitempty"`
		DashboardQuery *struct {
			Query string          `json:"query"`
			Input json.RawMessage `json:"input"`
		} `json:"dashboardQuery,omitempty"`
	}{
		DashboardChart: dashboardChartBody{
			DisplayName:     in.DisplayName,
			TileType:        tile,
			Description:     in.Description,
			ChartDatasource: in.ChartDatasource,
			Visualization:   in.Visualization,
			DrillDownConfig: in.DrillDownConfig,
		},
		ChartLayout: in.ChartLayout,
	}
	if in.Query != "" && in.Interval != nil {
		body.DashboardQuery = &struct {
			Query string          `json:"query"`
			Input json.RawMessage `json:"input"`
		}{Query: in.Query, Input: in.Interval}
	}

	var out json.RawMessage
	path := c.resourcePath("nativeDashboards/"+resourceID(dashboardID)+":addChart", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetChart returns a single dashboard chart by id (or full resource name). The
// endpoint is GET {instance}/dashboardCharts/<id>. The chart body is freeform,
// so it is returned as raw JSON.
func (c *Client) GetChart(ctx context.Context, chartID string) (json.RawMessage, error) {
	var out json.RawMessage
	path := c.resourcePath("dashboardCharts/"+resourceID(chartID), false)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BatchGetCharts fetches several dashboard charts in ONE call — the console's own
// dashboardCharts:batchGet — instead of one GetChart per id, so dereferencing a
// dashboard's charts costs a single request. ids may be bare ids or full resource
// names. Returns each chart's raw JSON.
//
// Note: batchGet is all-or-nothing — if any requested chart no longer exists the
// server returns a 500, so this is for healthy dashboards (copy/pull); callers
// that must isolate which chart is missing (e.g. verify) keep per-chart GetChart.
//
// Endpoint: GET {instance}/dashboardCharts:batchGet?names=<full>… (project ID form).
func (c *Client) BatchGetCharts(ctx context.Context, ids []string) ([]json.RawMessage, error) {
	q := url.Values{}
	for _, id := range ids {
		if id != "" {
			q.Add("names", c.qualify("dashboardCharts", id))
		}
	}
	var out struct {
		DashboardCharts []json.RawMessage `json:"dashboardCharts"`
	}
	if err := c.get(ctx, c.resourcePath("dashboardCharts:batchGet", false), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out.DashboardCharts, nil
}

// chartIDOf returns the bare id from a chart body's "name" field, or "".
func chartIDOf(raw json.RawMessage) string {
	var nm struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &nm) == nil {
		return resourceID(nm.Name)
	}
	return ""
}

// ChartsByID resolves chart ids to their raw bodies, keyed by bare id, using ONE
// dashboardCharts:batchGet when it succeeds and falling back to a per-chart
// GetChart when the batch fails — batchGet is all-or-nothing, so a dashboard with
// a dangling chart 500s it, and the fallback still resolves the charts that exist.
// So a healthy dashboard costs a single call while a broken one degrades to N. Ids
// that don't resolve (dangling) are simply absent from the map; the caller decides
// how to render a missing chart. Never returns an error — it is best-effort.
func (c *Client) ChartsByID(ctx context.Context, ids []string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(ids))
	if len(ids) == 0 {
		return out
	}
	if bodies, err := c.BatchGetCharts(ctx, ids); err == nil {
		for _, b := range bodies {
			if id := chartIDOf(b); id != "" {
				out[id] = b
			}
		}
		return out
	}
	for _, id := range ids {
		if b, err := c.GetChart(ctx, id); err == nil {
			out[lastSegment(id)] = b // match how callers (and the batch path) key by trailing id segment
		}
	}
	return out
}

// EditChartInput selects what to edit. dashboardQuery and/or dashboardChart are
// freeform JSON objects each carrying at least a "name" (bare id or full
// resource name; bare ids are qualified against the instance) plus the fields to
// change and an "etag". The editMask is built from the keys present in each
// object, honoring optimistic concurrency via the round-tripped etags.
type EditChartInput struct {
	DashboardQuery json.RawMessage // optional; e.g. {"name","query","input","etag"}
	DashboardChart json.RawMessage // optional; e.g. {"name","displayName",...,"etag"}
}

// queryEditableFields / chartEditableFields are the field paths the API accepts
// in editMask, mirroring DashboardQuery.update_fields / DashboardChart.update_fields.
var (
	queryEditableFields = []string{"query", "input"}
	chartEditableFields = []string{"display_name", "description", "tile_type", "visualization", "drill_down_config", "chart_datasource"}
)

// editMaskFor builds the snake_case mask entries (prefixed by parent) for the
// editable fields present in obj. obj's keys may be either camelCase (server
// form) or snake_case; both are recognized.
func editMaskFor(parent string, obj map[string]json.RawMessage, fields []string) []string {
	var mask []string
	for _, f := range fields {
		camel := snakeToCamel(f)
		if _, ok := obj[f]; ok {
			mask = append(mask, parent+"."+f)
			continue
		}
		if _, ok := obj[camel]; ok {
			mask = append(mask, parent+"."+f)
		}
	}
	return mask
}

// snakeToCamel converts a snake_case field name to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// withQualifiedName ensures the "name" field of a chart/query edit object is a
// fully-qualified resource name under collection, returning the rewritten object
// and its decoded key set.
func (c *Client) withQualifiedName(collection string, raw json.RawMessage) (json.RawMessage, map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, nil, fmt.Errorf("chronicle: edit chart %s is not a JSON object: %w", collection, err)
	}
	if nameRaw, ok := obj["name"]; ok {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err == nil && name != "" {
			q := c.qualify(collection, name)
			if q != name {
				b, _ := json.Marshal(q)
				obj["name"] = b
				rewritten, err := json.Marshal(obj)
				if err != nil {
					return nil, nil, err
				}
				return rewritten, obj, nil
			}
		}
	}
	return raw, obj, nil
}

// EditChart edits an existing chart and/or its query within a dashboard. The
// endpoint is POST {instance}/nativeDashboards/<id>:editChart. At least one of
// in.DashboardQuery / in.DashboardChart must be set.
func (c *Client) EditChart(ctx context.Context, dashboardID string, in EditChartInput) (json.RawMessage, error) {
	id := resourceID(dashboardID)
	body := struct {
		DashboardQuery json.RawMessage `json:"dashboardQuery,omitempty"`
		DashboardChart json.RawMessage `json:"dashboardChart,omitempty"`
		EditMask       string          `json:"editMask"`
	}{}
	var mask []string

	if len(in.DashboardQuery) > 0 {
		raw, obj, err := c.withQualifiedName("dashboardQueries", in.DashboardQuery)
		if err != nil {
			return nil, err
		}
		body.DashboardQuery = raw
		mask = append(mask, editMaskFor("dashboard_query", obj, queryEditableFields)...)
	}
	if len(in.DashboardChart) > 0 {
		raw, obj, err := c.withQualifiedName("dashboardCharts", in.DashboardChart)
		if err != nil {
			return nil, err
		}
		body.DashboardChart = raw
		mask = append(mask, editMaskFor("dashboard_chart", obj, chartEditableFields)...)
	}
	if len(in.DashboardQuery) == 0 && len(in.DashboardChart) == 0 {
		return nil, &APIError{
			Method: "POST",
			URL:    c.resourcePath("nativeDashboards/"+id+":editChart", false),
			Body:   "editChart requires a dashboardQuery and/or dashboardChart",
		}
	}
	// An object carrying only name/etag (no editable field) yields an empty mask;
	// posting editMask="" would update nothing yet still consume the etag. Refuse.
	if len(mask) == 0 {
		return nil, &APIError{
			Method: "POST",
			URL:    c.resourcePath("nativeDashboards/"+id+":editChart", false),
			Body:   "editChart: no editable fields present (expected one of query/input or display_name/description/tile_type/visualization/drill_down_config/chart_datasource)",
		}
	}
	body.EditMask = strings.Join(mask, ",")

	var out json.RawMessage
	path := c.resourcePath("nativeDashboards/"+id+":editChart", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveChart removes a chart from a dashboard. chartID is qualified against the
// instance when bare. The endpoint is POST
// {instance}/nativeDashboards/<id>:removeChart with {"dashboardChart": <name>}.
func (c *Client) RemoveChart(ctx context.Context, dashboardID, chartID string) (json.RawMessage, error) {
	body := struct {
		DashboardChart string `json:"dashboardChart"`
	}{DashboardChart: c.qualify("dashboardCharts", chartID)}

	var out json.RawMessage
	path := c.resourcePath("nativeDashboards/"+resourceID(dashboardID)+":removeChart", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExecuteQuery runs an ad-hoc dashboard query and returns the freeform result
// JSON. query is the UDM search string, interval the input-interval JSON
// (timeWindow or relativeTime), and filters an optional freeform JSON array.
// clearCache, when non-nil, forces a read from the database rather than cache.
// The endpoint is POST {instance}/dashboardQueries:execute.
func (c *Client) ExecuteQuery(ctx context.Context, query string, interval json.RawMessage, filters []json.RawMessage, clearCache *bool) (json.RawMessage, error) {
	body := struct {
		Query struct {
			Query string          `json:"query"`
			Input json.RawMessage `json:"input"`
		} `json:"query"`
		ClearCache *bool             `json:"clearCache,omitempty"`
		Filters    []json.RawMessage `json:"filters,omitempty"`
	}{ClearCache: clearCache, Filters: filters}
	body.Query.Query = query
	body.Query.Input = interval

	var out json.RawMessage
	if err := c.post(ctx, c.resourcePath("dashboardQueries:execute", false), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetQuery returns a stored dashboard query's details by id (or full resource
// name). The endpoint is GET {instance}/dashboardQueries/<id>. The body is
// freeform, so it is returned as raw JSON.
//
// DEVIATION: named GetQuery (the wrapper calls this get_execute_query, which is
// misleading — it is a plain GET, not an execution).
func (c *Client) GetQuery(ctx context.Context, queryID string) (json.RawMessage, error) {
	var out json.RawMessage
	path := c.resourcePath("dashboardQueries/"+resourceID(queryID), false)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}
