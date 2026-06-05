// LEGACY tier: Siemplify external API (/api/external/v1) case TASK surface —
// the checklist tasks attached to cases. Reads return RawJSON; writes take a
// freeform body and are LIVE MUTATIONS.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// GetCaseTask returns one case task by id.
func (c *Client) GetCaseTask(ctx context.Context, id string) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/tasks/"+url.PathEscape(id))
}

// CreateCaseTask creates a case task. LIVE MUTATION.
func (c *Client) CreateCaseTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/tasks", body)
}

// UpdateCaseTask updates a case task. LIVE MUTATION.
func (c *Client) UpdateCaseTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/cases/tasks", body)
}

// DeleteCaseTask deletes a case task by id. LIVE MUTATION; cannot be undone.
func (c *Client) DeleteCaseTask(ctx context.Context, id string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/cases/tasks/"+url.PathEscape(id), nil)
}

// MarkCaseTaskDone marks a case task done. body carries the task id. LIVE MUTATION.
func (c *Client) MarkCaseTaskDone(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/cases/tasks/MarkAsDone", body)
}

// AddOrUpdateCaseTask creates or updates a task on a case (case-scoped variant).
// LIVE MUTATION.
func (c *Client) AddOrUpdateCaseTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/AddOrUpdateCaseTask", body)
}

// ReassignCaseTask reassigns a case task to another user. LIVE MUTATION.
func (c *Client) ReassignCaseTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ReassignCaseTask", body)
}
