// LEGACY tier: the Siemplify external API (/api/external/v1) Case Management
// surface. This file covers the remaining /cases and /cases-queue operations not
// already mirrored elsewhere — case cards, comments, tasks, the case wall,
// reports, SLA controls, collaborator requests, and War Room incident creation.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External. Method names carry a "CaseX" prefix on otherwise-generic
// verbs to stay globally unique across the shared *Client.
package legacy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// CaseXGetCardsByRequest returns minimal CaseCard data for cases matching the
// request. Read-only despite the POST verb. body is the freeform legacy payload.
func (c *Client) CaseXGetCardsByRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/GetCaseCardsByRequest", body)
}

// CaseXGetWallItemsForWarRoom returns all wall items (case history) for a case
// for transfer into a Command Center incident, by integer case id.
func (c *Client) CaseXGetWallItemsForWarRoom(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/GetWallItemsForWarRoom/"+strconv.Itoa(id))
}

// CaseXCreateWarRoomIncident creates a Command Center incident from a given case.
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXCreateWarRoomIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateWarRoomIncidentFromCase", body)
}

// CaseXExecuteBulkClose performs a bulk close-case action over several cases at
// once. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXExecuteBulkClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkCloseCase", body)
}

// CaseXValidateBulkCloseAssignees validates case assignees before a bulk
// close-case action. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXValidateBulkCloseAssignees(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases-queue/bulk-operations/ValidateCasesAssigneeForCloseCase", body)
}

// CaseXExecuteStep executes a single playbook step (triggering re-calculation).
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXExecuteStep(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteStep", body)
}

// CaseXGenerateReport generates a report for a specific case. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXGenerateReport(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/GenerateCaseReport", body)
}

// CaseXPauseAlertSla pauses the SLA on an alert. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) CaseXPauseAlertSla(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/PauseAlertSla", body)
}

// CaseXResumeAlertSla resumes a previously-paused SLA on an alert. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXResumeAlertSla(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ResumeAlertSla", body)
}

// CaseXListComments returns all case comments matching the filter. q carries the
// filter keys (CaseId, AlertIdentifier, UserOwnerId, Spec.*).
func (c *Client) CaseXListComments(ctx context.Context, q url.Values) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/cases/comments", q)
}

// CaseXCreateComment creates a case comment. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) CaseXCreateComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/comments", body)
}

// CaseXUpdateComment updates an existing case comment by integer id. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXUpdateComment(ctx context.Context, id int, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/cases/comments/"+strconv.Itoa(id), body)
}

// CaseXMarkCommentDeleted marks a case comment as deleted by integer id.
// LIVE MUTATION.
func (c *Client) CaseXMarkCommentDeleted(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPatch, "/cases/comments/mark-as-deleted/"+strconv.Itoa(id), nil)
}

// CaseXDownloadCommentFile downloads the file attached to a comment, by the
// integer file id.
func (c *Client) CaseXDownloadCommentFile(ctx context.Context, fileID int) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/comments/file/"+strconv.Itoa(fileID))
}

// CaseXSetWallItemFavourite marks a case wall item as favourite. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXSetWallItemFavourite(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPatch, "/cases/wall/favourite", body)
}

// CaseXListTasksByRequest returns case tasks matching the request. q carries the
// pagination and filter keys (RequestedPage, PageSize, SearchTerm, Filters,
// StatusFilter, SortBy).
func (c *Client) CaseXListTasksByRequest(ctx context.Context, q url.Values) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/cases/tasks/GetByRequest", q)
}

// CaseXGetTasksCountForUser returns the task count for the logged-in user.
func (c *Client) CaseXGetTasksCountForUser(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/tasks/GetTasksCountForUser")
}

// CaseXGenerateCollaboratorRequest generates an alert of the request template in
// the environment. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) CaseXGenerateCollaboratorRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/requests/GenerateCollaboratorRequest", body)
}

// CaseXListCollaboratorRequests returns collaborator requests matching the
// request. q carries the pagination and filter keys (RequestedPage, PageSize,
// SearchTerm, Filters, SortBy).
func (c *Client) CaseXListCollaboratorRequests(ctx context.Context, q url.Values) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/cases/requests/GetByRequest", q)
}

// CaseXGetCollaboratorRequestCount returns the collaborator-request count.
func (c *Client) CaseXGetCollaboratorRequestCount(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/requests/GetCollaboratorRequestCount")
}
