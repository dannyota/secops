package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Native-dashboard write surface (create/update/delete/duplicate/import,
// chart add/get/edit/remove, and dashboard-query execute). All endpoints hang
// off the instance path using the project ID (string) form — numeric=false —
// matching ListNativeDashboards/ExportDashboard in dashboards.go and the
// wrapper, which builds every nativeDashboards/dashboardCharts/dashboardQueries
// URL from the string project_id.

// Dashboard access types (the "access" field on a native dashboard).
const (
	DashboardPublic  = "DASHBOARD_PUBLIC"
	DashboardPrivate = "DASHBOARD_PRIVATE"
)

// Dashboard views passed via ?view= on GetDashboard.
const (
	dashboardViewBasic = "NATIVE_DASHBOARD_VIEW_BASIC"
	dashboardViewFull  = "NATIVE_DASHBOARD_VIEW_FULL"
)

// Chart tile types.
const (
	TileTypeVisualization = "TILE_TYPE_VISUALIZATION"
	TileTypeButton        = "TILE_TYPE_BUTTON"
)

// resourceID extracts the bare ID from a value that may be a fully-qualified
// resource name (projects/.../<collection>/<id>) or already a bare ID. Mirrors
// the wrapper's format_resource_id.
func resourceID(s string) string {
	if strings.HasPrefix(s, "projects/") {
		parts := strings.Split(s, "/")
		return parts[len(parts)-1]
	}
	return s
}

// qualify returns name under the instance path + "/" + collection if it is not
// already a fully-qualified resource name. Mirrors the wrapper's
// "{instance_id}/<collection>/<id>" prefixing for chart/query references.
func (c *Client) qualify(collection, name string) string {
	if isQualifiedName(name) {
		return name
	}
	return c.instancePath(false) + "/" + collection + "/" + resourceID(name)
}

// DashboardDefinition is the inner "definition" of a native dashboard: its
// filters and chart layout. Both halves are freeform server JSON, so they are
// kept as raw lists of objects.
type DashboardDefinition struct {
	Filters []json.RawMessage `json:"filters,omitempty"`
	Charts  []json.RawMessage `json:"charts,omitempty"`
}

// CreateDashboard creates a CUSTOM native dashboard.
//
// accessType is DashboardPublic or DashboardPrivate. filters and charts are
// optional freeform JSON arrays (each element a server-shaped object); pass nil
// to omit. The endpoint is POST {instance}/nativeDashboards.
func (c *Client) CreateDashboard(ctx context.Context, displayName, description, accessType string, filters, charts []json.RawMessage) (*NativeDashboard, error) {
	var def *DashboardDefinition
	if len(filters) > 0 || len(charts) > 0 {
		def = &DashboardDefinition{Filters: filters, Charts: charts}
	}
	body := struct {
		DisplayName string               `json:"displayName"`
		Description string               `json:"description,omitempty"`
		Access      string               `json:"access,omitempty"`
		Type        string               `json:"type"`
		Definition  *DashboardDefinition `json:"definition,omitempty"`
	}{
		DisplayName: displayName,
		Description: description,
		Access:      accessType,
		Type:        "CUSTOM",
		Definition:  def,
	}
	var out NativeDashboard
	if err := c.post(ctx, c.resourcePath("nativeDashboards", false), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDashboard fetches a single dashboard. When full is true the FULL view
// (charts + queries inlined) is requested; otherwise BASIC.
//
// dashboardID may be a bare id or a fully-qualified resource name.
func (c *Client) GetDashboard(ctx context.Context, dashboardID string, full bool) (*NativeDashboard, error) {
	view := dashboardViewBasic
	if full {
		view = dashboardViewFull
	}
	q := url.Values{"view": {view}}
	var out NativeDashboard
	path := c.resourcePath("nativeDashboards/"+resourceID(dashboardID), false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// DashboardUpdate carries the optionally-mutable fields of a dashboard. Only
// the fields set here are sent, and the updateMask is derived from them. A nil
// pointer means "leave unchanged"; for filters/charts a non-nil (possibly
// empty) slice means "replace".
//
// DEVIATION: pointer/slice presence drives the mask, vs the wrapper's
// "is not None" checks and string-or-list filter coercion done at the call site.
type DashboardUpdate struct {
	DisplayName *string
	Description *string
	Filters     []json.RawMessage // non-nil replaces definition.filters
	Charts      []json.RawMessage // non-nil replaces definition.charts
}

// UpdateDashboard patches a dashboard, sending only the fields set on upd and an
// updateMask covering exactly those fields. The endpoint is PATCH
// {instance}/nativeDashboards/<id>?updateMask=...
func (c *Client) UpdateDashboard(ctx context.Context, dashboardID string, upd DashboardUpdate) (*NativeDashboard, error) {
	id := resourceID(dashboardID)
	body := struct {
		DisplayName *string              `json:"displayName,omitempty"`
		Description *string              `json:"description,omitempty"`
		Definition  *DashboardDefinition `json:"definition,omitempty"`
	}{}
	var mask []string

	if upd.DisplayName != nil {
		body.DisplayName = upd.DisplayName
		mask = append(mask, "display_name")
	}
	if upd.Description != nil {
		body.Description = upd.Description
		mask = append(mask, "description")
	}
	if upd.Filters != nil || upd.Charts != nil {
		def := &DashboardDefinition{}
		if upd.Filters != nil {
			def.Filters = upd.Filters
			mask = append(mask, "definition.filters")
		}
		if upd.Charts != nil {
			def.Charts = upd.Charts
			mask = append(mask, "definition.charts")
		}
		body.Definition = def
	}
	if len(mask) == 0 {
		return nil, &APIError{
			Method: "PATCH",
			URL:    c.resourcePath("nativeDashboards/"+id, false),
			Body:   "no dashboard fields provided to update",
		}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var out NativeDashboard
	if err := c.patch(ctx, c.resourcePath("nativeDashboards/"+id, false), body, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDashboard deletes a dashboard by id (or full resource name).
func (c *Client) DeleteDashboard(ctx context.Context, dashboardID string) error {
	path := c.resourcePath("nativeDashboards/"+resourceID(dashboardID), false)
	return c.do(ctx, "DELETE", path, nil, nil)
}

// DuplicateDashboard copies an existing dashboard into a new CUSTOM dashboard
// with the given displayName/accessType. The endpoint is POST
// {instance}/nativeDashboards/<id>:duplicate.
func (c *Client) DuplicateDashboard(ctx context.Context, dashboardID, displayName, accessType, description string) (*NativeDashboard, error) {
	type nd struct {
		DisplayName string `json:"displayName"`
		Access      string `json:"access,omitempty"`
		Type        string `json:"type"`
		Description string `json:"description,omitempty"`
	}
	body := struct {
		NativeDashboard nd `json:"nativeDashboard"`
	}{NativeDashboard: nd{
		DisplayName: displayName,
		Access:      accessType,
		Type:        "CUSTOM",
		Description: description,
	}}
	var out NativeDashboard
	path := c.resourcePath("nativeDashboards/"+resourceID(dashboardID)+":duplicate", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// _validImportKeys are the keys the import payload's dashboard object must
// contain at least one of, matching the wrapper's guard.
var _validImportKeys = []string{"dashboard", "dashboardCharts", "dashboardQueries"}

// ImportDashboard imports a single native dashboard from an export-shaped JSON
// object. payload is the dashboard object itself (containing at least one of
// "dashboard", "dashboardCharts", "dashboardQueries"); it is wrapped in the
// {"source": {"dashboards": [payload]}} envelope the API expects. The endpoint
// is POST {instance}/nativeDashboards:import.
func (c *Client) ImportDashboard(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return nil, fmt.Errorf("chronicle: import dashboard payload is not a JSON object: %w", err)
	}
	ok := false
	for _, k := range _validImportKeys {
		if _, present := probe[k]; present {
			ok = true
			break
		}
	}
	if !ok {
		return nil, &APIError{
			Method: "POST",
			URL:    c.resourcePath("nativeDashboards:import", false),
			Body:   "dashboard must contain at least one of: " + strings.Join(_validImportKeys, ", "),
		}
	}

	body := struct {
		Source struct {
			Dashboards []json.RawMessage `json:"dashboards"`
		} `json:"source"`
	}{}
	body.Source.Dashboards = []json.RawMessage{payload}

	var out json.RawMessage
	if err := c.post(ctx, c.resourcePath("nativeDashboards:import", false), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

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
