// LEGACY tier: Siemplify external API (/api/external/v1) BULK case operations.
//
// Each method mutates many cases in one call — LIVE and high-blast-radius;
// confirm the id set first. body carries the SOAR INTEGER case ids. The
// cases-queue bulk-close is in cases.go (BulkCloseCases).
package legacy

import "context"

// BulkAddCaseTag tags many cases at once.
func (c *Client) BulkAddCaseTag(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkAddCaseTag", body)
}

// BulkAssign assigns many cases to a user at once.
func (c *Client) BulkAssign(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkAssign", body)
}

// BulkChangeCasePriority changes the priority of many cases at once.
func (c *Client) BulkChangeCasePriority(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkChangeCasePriority", body)
}

// BulkChangeCaseStage moves many cases to a stage at once.
func (c *Client) BulkChangeCaseStage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkChangeCaseStage", body)
}

// BulkReopenCase reopens many cases at once.
func (c *Client) BulkReopenCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteBulkReopenCase", body)
}

// BulkAssignCaseQueue assigns many queued cases at once (cases-queue variant).
func (c *Client) BulkAssignCaseQueue(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases-queue/bulk-operations/ExecuteBulkAssignCase", body)
}

// MergeCases merges multiple cases into one. body carries the source/target ids.
func (c *Client) MergeCases(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases-queue/bulk-operations/MergeCases", body)
}
