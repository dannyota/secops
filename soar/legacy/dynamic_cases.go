// LEGACY tier: Siemplify external API (/api/external/v1) DYNAMIC-CASES surface —
// the newer case API used by the modern case view. Many actions mirror the
// /cases surface (see cases_actions.go); only the dynamic-unique reads and
// actions are exposed here, prefixed Dynamic* where they would otherwise collide.
// caseID parameters are the SOAR INTEGER case id. Reads return RawJSON; writes
// take a freeform body and are LIVE MUTATIONS.
package legacy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// --- dynamic-case reads ---

// DynamicGetCaseDetails returns the modern case-detail blob for a case.
func (c *Client) DynamicGetCaseDetails(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/dynamic-cases/GetCaseDetails/"+strconv.Itoa(caseID))
}

// DynamicGetEvidenceData returns a piece of case evidence (dynamic surface).
func (c *Client) DynamicGetEvidenceData(ctx context.Context, evidenceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/dynamic-cases/GetEvidenceData/"+url.PathEscape(evidenceID))
}

// GetWallActivities returns the case-wall activities for a case.
func (c *Client) GetWallActivities(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/dynamic-cases/GetWallActivities/"+strconv.Itoa(caseID))
}

// GetWallActivitiesV2 returns the v2 case-wall activities for a case.
func (c *Client) GetWallActivitiesV2(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/dynamic-cases/GetWallActivitiesV2/"+strconv.Itoa(caseID))
}

// GetCaseWallActivities returns filtered case-wall activities. body is the
// freeform filter payload.
func (c *Client) GetCaseWallActivities(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/GetCaseWallActivities", body)
}

// GetCaseWallActivitiesCount returns the count of matching wall activities.
func (c *Client) GetCaseWallActivitiesCount(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/GetCaseWallActivities/count", body)
}

// GetAlertEvents returns the events of an alert. body selects the alert.
func (c *Client) GetAlertEvents(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/GetAlertEvents", body)
}

// DynamicIsCaseUpdated checks for case changes (dynamic surface).
func (c *Client) DynamicIsCaseUpdated(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/IsCaseUpdated", body)
}

// --- dynamic-unique actions ---

// MoveAlertToNewCase splits an alert out into a new case. LIVE MUTATION.
func (c *Client) MoveAlertToNewCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/MoveAlertToNewCase", body)
}

// ReopenAlert reopens a closed alert. LIVE MUTATION.
func (c *Client) ReopenAlert(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/ReopenAlert", body)
}

// IngestCaseInOtherEnvironment moves/ingests a case into another environment.
// LIVE MUTATION.
func (c *Client) IngestCaseInOtherEnvironment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/IngestCaseInOtherEnvironment", body)
}

// AddOrUpdateEntityProperty sets a property on a case entity. LIVE MUTATION.
func (c *Client) AddOrUpdateEntityProperty(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddOrUpdateEntityProperty", body)
}

// UpdateComment edits an existing case comment. LIVE MUTATION.
func (c *Client) UpdateComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/dynamic-cases/UpdateComment", body)
}

// MarkCommentAsDeleted soft-deletes a case comment by id. LIVE MUTATION.
func (c *Client) MarkCommentAsDeleted(ctx context.Context, id string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/dynamic-cases/MarkCommentAsDeleted/"+url.PathEscape(id), nil)
}
