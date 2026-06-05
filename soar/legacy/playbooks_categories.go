// LEGACY tier: the Siemplify external API (/api/external/v1) playbook *category*
// surface — the folders that organize workflow definitions. Reads return
// RawJSON; writes take a freeform body.
package legacy

import "context"

// ListWorkflowCategories returns every playbook (workflow) category.
func (c *Client) ListWorkflowCategories(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetWorkflowCategories")
}

// AddOrUpdatePlaybookCategory creates or renames a playbook category. body is the
// freeform category payload. LIVE MUTATION.
func (c *Client) AddOrUpdatePlaybookCategory(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/AddOrUpdatePlaybookCategory", body)
}

// RemovePlaybookCategories deletes playbook categories. body carries the ids.
// LIVE MUTATION.
func (c *Client) RemovePlaybookCategories(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/RemoveCategories", body)
}

// MoveDefinitionsToCategory moves workflow definitions into a category. body
// carries the definition ids and target category. LIVE MUTATION.
func (c *Client) MoveDefinitionsToCategory(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/MoveDefinitionsToCategory", body)
}
