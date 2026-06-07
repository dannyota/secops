// LEGACY tier: the Siemplify external API (/api/external/v1) Case Management
// surface for case overviews and predefined widgets.
//
// Case overviews render a case's alerts, entities, and configurable widgets into
// the templated layout shown in the SOAR case view; predefined widgets expose the
// built-in case widget catalog. These endpoints back that view and the templates
// that drive it. This is the reliable external-API path for case overviews.
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

// CaseOverviewGetAlertData returns overview data for one alert. body is the
// freeform request payload selecting the alert.
func (c *Client) CaseOverviewGetAlertData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/GetAlertOverviewData", body)
}

// CaseOverviewGetAlertsEntities returns the entities associated with the
// requested alerts. body is the freeform request payload.
func (c *Client) CaseOverviewGetAlertsEntities(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/GetAlertsEntities", body)
}

// CaseOverviewGetCaseEntities returns the entities associated with a case by its
// numeric case id.
func (c *Client) CaseOverviewGetCaseEntities(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/case-overview/GetCaseEntities/"+strconv.Itoa(caseID))
}

// CaseOverviewGetData returns the rendered overview data for a case. body is the
// freeform request payload selecting the case and template.
func (c *Client) CaseOverviewGetData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/GetCaseOverviewData", body)
}

// CaseOverviewGetFullTemplateDetails returns the full definition of one overview
// template by its identifier.
func (c *Client) CaseOverviewGetFullTemplateDetails(ctx context.Context, templateIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/case-overview/GetFullOverviewTemplateDetails/"+url.PathEscape(templateIdentifier))
}

// CaseOverviewGetTemplate returns an overview template. body is the freeform
// request payload selecting the template.
func (c *Client) CaseOverviewGetTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/GetOverviewTemplate", body)
}

// CaseOverviewListTemplateCards returns basic info for each available overview
// template.
func (c *Client) CaseOverviewListTemplateCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/case-overview/GetOverviewTemplateCards")
}

// CaseOverviewPreviewWidget renders a preview of an overview widget without
// persisting it. body is the freeform widget definition. LIVE MUTATION.
func (c *Client) CaseOverviewPreviewWidget(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/PreviewOverviewWidget", body)
}

// CaseOverviewResolveWidget resolves an overview widget's values for a case.
// body is the freeform widget+context payload. LIVE MUTATION.
func (c *Client) CaseOverviewResolveWidget(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/ResolveOverviewWidget", body)
}

// CaseOverviewSaveTemplate creates or updates an overview template. body is the
// freeform template payload. LIVE MUTATION.
func (c *Client) CaseOverviewSaveTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/case-overview/SaveOverviewTemplate", body)
}

// CaseOverviewListPredefinedWidgets returns all built-in case predefined widgets.
func (c *Client) CaseOverviewListPredefinedWidgets(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/predefined-widgets/cases")
}
