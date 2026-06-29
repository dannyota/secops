// LEGACY tier: the Siemplify external API (/api/external/v1) Case Management
// surface, /dynamic-cases operations. These mirror much of the /cases surface
// against the dynamic-cases data path (case mutations, tags, comments, evidence,
// tasks, reports, and the War Room transfer), excluding the ones already
// implemented elsewhere in this package.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body. All methods speak the
// AppKey-authenticated external API via c.t.External. Method names carry a
// "DynamicCaseX" prefix to stay globally unique across the shared *Client.
package legacy

import (
	"context"
	"strconv"
)

// DynamicCaseXAddCaseComment adds a comment to a case. body is the freeform
// legacy payload. Deprecated upstream in favour of AddComment. LIVE MUTATION.
func (c *Client) DynamicCaseXAddCaseComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddCaseComment", body)
}

// DynamicCaseXAddComment adds a comment (optionally with attachment) to a case.
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXAddComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddComment", body)
}

// DynamicCaseXAddTag adds a tag to a case for later filtering. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXAddTag(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddCaseTag", body)
}

// DynamicCaseXRemoveTag removes a tag from a case. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) DynamicCaseXRemoveTag(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/RemoveCaseTag", body)
}

// DynamicCaseXAddEvidence adds an evidence (attachment) to a case. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXAddEvidence(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddEvidence", body)
}

// DynamicCaseXAddOrUpdateTask adds or updates a task assigned to a user in a
// case. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXAddOrUpdateTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AddOrUpdateCaseTask", body)
}

// DynamicCaseXMarkTaskDone marks a case task as done by the logged-in user. body
// is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXMarkTaskDone(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/MarkTaskAsDone", body)
}

// DynamicCaseXAssignUser assigns a specific user to a case. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXAssignUser(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/AssignUserToCase", body)
}

// DynamicCaseXChangeDescription changes the description of a case. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXChangeDescription(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/ChangeCaseDescription", body)
}

// DynamicCaseXChangeImportanceStatus changes the "is important" status of a
// case. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXChangeImportanceStatus(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/ChangeCaseImportanceStatus", body)
}

// DynamicCaseXChangeStage changes the case handling stage. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXChangeStage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/ChangeCaseStage", body)
}

// DynamicCaseXRename changes the case title. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) DynamicCaseXRename(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/RenameCase", body)
}

// DynamicCaseXUpdateCasePriority changes the priority of a case. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXUpdateCasePriority(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/UpdateCasePriority", body)
}

// DynamicCaseXUpdateAlertPriority changes the priority of an alert. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXUpdateAlertPriority(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/UpdateAlertPriority", body)
}

// DynamicCaseXCloseAlert closes an alert in a case. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) DynamicCaseXCloseAlert(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/CloseAlert", body)
}

// DynamicCaseXClose closes a case. body is the freeform legacy payload.
// LIVE MUTATION.
func (c *Client) DynamicCaseXClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/CloseCase", body)
}

// DynamicCaseXExecuteBulkReopen reopens alert cases in bulk. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXExecuteBulkReopen(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/ExecuteBulkReopenCase", body)
}

// DynamicCaseXCreate injects a case into the Siemplify Data Processing Engine.
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXCreate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/CreateCase", body)
}

// DynamicCaseXCreateEntity manually adds an entity to an alert in a case. body
// is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXCreateEntity(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/CreateCaseEntity", body)
}

// DynamicCaseXRaiseIncident raises a case to the "Incident" stage. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXRaiseIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/RaiseIncident", body)
}

// DynamicCaseXUnraiseIncident removes the incident flag from a case. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXUnraiseIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/UnraiseIncident", body)
}

// DynamicCaseXGenerateReport generates a case report. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXGenerateReport(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/GenerateCaseReport", body)
}

// DynamicCaseXGenerateReportBytes is like DynamicCaseXGenerateReport but returns
// the raw bytes (the response may be binary content, not JSON).
func (c *Client) DynamicCaseXGenerateReportBytes(ctx context.Context, body any) ([]byte, error) {
	return c.t.ExternalBytes(ctx, "POST", "/dynamic-cases/GenerateCaseReport", body)
}

// DynamicCaseXRequest adds an evidence (attachment) to a case via the request
// endpoint. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) DynamicCaseXRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/dynamic-cases/request", body)
}

// DynamicCaseXGetWallActivitiesForCommandCenter returns the wall items (case
// history) transferred into the Command Center incident, by integer id.
func (c *Client) DynamicCaseXGetWallActivitiesForCommandCenter(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/dynamic-cases/GetWallActivitiesForCommandCenter/"+strconv.Itoa(id))
}
