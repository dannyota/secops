// LEGACY tier: the Siemplify external API (/api/external/v1) Reports surface.
//
// These endpoints manage advanced report templates, the widgets they contain,
// and the schedules that generate and share them. They predate the modern
// reporting model and are kept here until it covers them.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"strconv"
)

// ReportGetTemplates returns all available advanced report templates.
func (c *Client) ReportGetTemplates(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/reports/GetReportTemplates")
}

// ReportRefreshAdvanced recomputes the advanced reports and returns the result.
func (c *Client) ReportRefreshAdvanced(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/reports/RefreshAdvancedReports")
}

// ReportGetSchedules returns the report schedules matching the request. body is
// the freeform legacy filter payload.
func (c *Client) ReportGetSchedules(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/GetReportSchedules", body)
}

// ReportAddOrUpdateTemplate creates a new report template or updates an existing
// one. body is the freeform report-template payload. LIVE MUTATION.
func (c *Client) ReportAddOrUpdateTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/AddOrUpdateReportTemplate", body)
}

// ReportAddOrUpdateWidget creates a new report widget or updates an existing one.
// body is the freeform report-widget payload. LIVE MUTATION.
func (c *Client) ReportAddOrUpdateWidget(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/AddOrUpdateReportWidget", body)
}

// ReportAddOrUpdateSchedule creates a new report schedule or updates an existing
// one. body is the freeform report-schedule payload. LIVE MUTATION.
func (c *Client) ReportAddOrUpdateSchedule(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/AddOrUpdateReportSchedule", body)
}

// ReportDuplicateTemplate copies an existing report template into a new one.
// body is the freeform legacy payload selecting the source template. LIVE MUTATION.
func (c *Client) ReportDuplicateTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/DuplicateReportTemplate", body)
}

// ReportGenerateTemplate generates a report from a template. body is the freeform
// generation payload. LIVE MUTATION.
func (c *Client) ReportGenerateTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/GenerateReportTemplate", body)
}

// ReportImportTemplate imports a report template from an exported definition.
// body is the freeform template-definition payload. LIVE MUTATION.
func (c *Client) ReportImportTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/ImportReportTemplate", body)
}

// ReportShareAdvanced shares an advanced report. body is the freeform sharing
// payload. LIVE MUTATION.
func (c *Client) ReportShareAdvanced(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/reports/ShareAdvancedReport", body)
}

// ReportDeleteSchedule deletes a report schedule by its numeric id. Despite the
// HTTP GET verb, this is a LIVE MUTATION; it cannot be undone.
func (c *Client) ReportDeleteSchedule(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodGet, "/reports/DeleteReportSchedule/"+strconv.Itoa(id), nil)
}

// ReportRemoveTemplate deletes a report template by its numeric id. Despite the
// HTTP GET verb, this is a LIVE MUTATION; it cannot be undone.
func (c *Client) ReportRemoveTemplate(ctx context.Context, templateID int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodGet, "/reports/RemoveReportTemplate/"+strconv.Itoa(templateID), nil)
}

// ReportRemoveWidget deletes a report widget by its numeric id. Despite the HTTP
// GET verb, this is a LIVE MUTATION; it cannot be undone.
func (c *Client) ReportRemoveWidget(ctx context.Context, widgetID int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodGet, "/reports/RemoveReportWidget/"+strconv.Itoa(widgetID), nil)
}
