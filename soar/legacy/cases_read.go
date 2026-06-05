// LEGACY tier: Siemplify external API (/api/external/v1) case READ surface.
//
// Read-only case lookups. caseID parameters are the SOAR INTEGER case id (see the
// dual case-id gotcha in cases.go). Reads return RawJSON.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// GetCaseExists reports whether a case id exists.
func (c *Client) GetCaseExists(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/GetCaseExists/"+strconv.Itoa(caseID))
}

// GetCaseWall returns the case wall (timeline of items) for a case.
func (c *Client) GetCaseWall(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/wall/"+strconv.Itoa(caseID))
}

// GetCaseInsights returns the insights attached to a case.
func (c *Client) GetCaseInsights(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/insights/"+strconv.Itoa(caseID))
}

// GetActionResult returns a stored action result by its (string) id.
func (c *Client) GetActionResult(ctx context.Context, resultID string) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/GetActionResultById/"+url.PathEscape(resultID))
}

// GetEvidenceData returns a piece of case evidence by id.
func (c *Client) GetEvidenceData(ctx context.Context, evidenceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/GetEvidenceData/"+url.PathEscape(evidenceID))
}

// IsCaseUpdated checks whether cases changed since a prior snapshot. body carries
// the case ids and timestamps.
func (c *Client) IsCaseUpdated(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/IsCaseUpdated", body)
}

// GetWorkflowInstanceSummary returns the playbook-run summary for a case. body
// carries the case/alert selector.
func (c *Client) GetWorkflowInstanceSummary(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/GetWorkflowInstanceSummary", body)
}

// GetAlertNames returns the distinct alert names in the queue. body is the
// freeform filter payload.
func (c *Client) GetAlertNames(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases-queue/GetAlertNames", body)
}

// ListAlertVendors returns the alert vendors seen in the queue.
func (c *Client) ListAlertVendors(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/cases-queue/alert-vendors")
}

// ListSavedFilters returns the saved case-queue filters.
func (c *Client) ListSavedFilters(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/cases-queue/saved-filter")
}
