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
	TileTypeMarkdown      = "TILE_TYPE_MARKDOWN"
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

// GetDashboard fetches a single dashboard. When full is true the FULL view (the
// complete definition: chart references, layout, filters) is requested; otherwise
// BASIC (the definition is a stub — no charts). Note the FULL view does NOT inline
// the chart query bodies: definition.charts[].dashboardChart is a resource-name
// reference to a separate dashboardChart, whose chartDatasource.dashboardQuery is a
// further reference to a dashboardQuery (the YARA-L). Use GetChart/GetQuery to
// dereference, or :addChart/:editChart to author them.
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
	Access      *string           // DashboardPublic or DashboardPrivate
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
		Access      *string              `json:"access,omitempty"`
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
	if upd.Access != nil {
		body.Access = upd.Access
		mask = append(mask, "access")
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
// with the given displayName/accessType. The server mints the copy its own
// independent charts and queries — no chart or query id is shared with the
// source — so the copy renders and deletes like any other dashboard. A single
// call; see DeepCopyDashboard for the client-side equivalent. The endpoint is
// POST {instance}/nativeDashboards/<id>:duplicate.
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

// ValidImportKeys are the keys an import payload's dashboard object must
// contain at least one of, matching the wrapper's guard. Exported so callers
// that pre-shape an import document (e.g. the CLI's export/import) check against
// the same allow-list ImportDashboard enforces.
var ValidImportKeys = []string{"dashboard", "dashboardCharts", "dashboardQueries"}

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
	for _, k := range ValidImportKeys {
		if _, present := probe[k]; present {
			ok = true
			break
		}
	}
	if !ok {
		return nil, &APIError{
			Method: "POST",
			URL:    c.resourcePath("nativeDashboards:import", false),
			Body:   "dashboard must contain at least one of: " + strings.Join(ValidImportKeys, ", "),
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
