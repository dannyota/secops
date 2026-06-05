// LEGACY tier: the Siemplify external API (/api/external/v1) Dashboards surface.
//
// Dashboards are the SOC home-screen reports built from widgets; each widget
// renders a query (case counts, SLA, entity stats) against a definition. These
// endpoints manage dashboards and their widgets, expose the available widget
// definitions, and resolve widget values/case ids for rendering.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// DashboardWidgetDefinitionList returns all available widget definitions
// (independent of any specific dashboard).
func (c *Client) DashboardWidgetDefinitionList(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/dashboards/GetDashboardWidgetDefinitions")
}

// DashboardGetWidgetValues resolves the rendered values for a widget. body is the
// freeform legacy widget-query payload.
func (c *Client) DashboardGetWidgetValues(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/GetDashboardWidgetValues", body)
}

// DashboardGetWidgetCaseIds returns the case ids backing a widget's value. body is
// the freeform legacy widget-query payload.
func (c *Client) DashboardGetWidgetCaseIds(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/GetDashboardWidgetCaseIds", body)
}

// DashboardAddOrUpdate creates a new dashboard or updates an existing one. body is
// the freeform dashboard payload. LIVE MUTATION.
func (c *Client) DashboardAddOrUpdate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/AddOrUpdateDashboard", body)
}

// DashboardAddOrUpdateWidget creates a new dashboard widget or updates an existing
// one. body is the freeform widget payload. LIVE MUTATION.
func (c *Client) DashboardAddOrUpdateWidget(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/AddOrUpdateDashboardWidget", body)
}

// DashboardImport imports a dashboard from an exported definition. body is the
// freeform dashboard-import payload. LIVE MUTATION.
func (c *Client) DashboardImport(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/ImportDashboard", body)
}

// DashboardSaveAsReportTemplate saves a dashboard as a reusable report template.
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DashboardSaveAsReportTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dashboards/SaveDashboardAsReportTemplate", body)
}

// DashboardDelete deletes a dashboard by id. This cannot be undone. LIVE MUTATION.
func (c *Client) DashboardDelete(ctx context.Context, dashboardID int) (RawJSON, error) {
	q := url.Values{"dashboardtId": {strconv.Itoa(dashboardID)}}
	return c.externalPost(ctx, "/dashboards/DeleteDashboard?"+q.Encode(), nil)
}

// DashboardDeleteWidget deletes a dashboard widget by id. This cannot be undone.
// LIVE MUTATION.
func (c *Client) DashboardDeleteWidget(ctx context.Context, widgetID int) (RawJSON, error) {
	q := url.Values{"dashboardWidgetId": {strconv.Itoa(widgetID)}}
	return c.externalPost(ctx, "/dashboards/DeleteDashboardWidget?"+q.Encode(), nil)
}
