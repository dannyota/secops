// LEGACY tier: the Siemplify external API (/api/external/v1) Resources surface.
//
// These endpoints fetch shared "resource" artifacts by id — action results, full
// case details, and entity insights — plus an audit-actions CSV export. They are
// read-only GETs — the reliable external-API path for these shared resources.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage (the caller decodes only the fields it needs). All methods speak
// the AppKey-authenticated external API via c.t.External.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// ResourceDownloadAuditControllerActionsCsv returns the audit-controller actions
// log as CSV.
func (c *Client) ResourceDownloadAuditControllerActionsCsv(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/resources/DownloadAuditControllerActionsCsv")
}

// ResourceGetActionResultsById returns the result of one action by its result id
// within the given case.
func (c *Client) ResourceGetActionResultsById(ctx context.Context, caseID int, actionResultID string) (RawJSON, error) {
	return c.externalGet(ctx, "/resources/GetActionResultsById/"+strconv.Itoa(caseID)+"/"+url.PathEscape(actionResultID))
}

// ResourceGetCaseFullDetailsById returns the full details of a case by its id,
// scoped to the given parent id.
func (c *Client) ResourceGetCaseFullDetailsById(ctx context.Context, parentID string, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/resources/GetCaseFullDetailsById/"+url.PathEscape(parentID)+"/"+strconv.Itoa(caseID))
}

// ResourceGetEntityInsightsById returns the entity insights identified by
// insightsId within the given case.
func (c *Client) ResourceGetEntityInsightsById(ctx context.Context, caseID, insightsID int) (RawJSON, error) {
	return c.externalGet(ctx, "/resources/GetEntityInsightsById/"+strconv.Itoa(caseID)+"/"+strconv.Itoa(insightsID))
}
