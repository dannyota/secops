// LEGACY tier: Siemplify external API (/api/external/v1) LIST settings — block
// lists (model blocks), tracking lists, and custom lists used by playbooks and
// enrichment. Config-as-code. Reads return RawJSON; writes take a freeform body.
package legacy

import "context"

// GetAllModelBlockRecords returns every block-list (model block) record.
func (c *Client) GetAllModelBlockRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetAllModelBlockRecords")
}

// GetBlockListDetails returns details for specific block-list records. body is
// the freeform selector payload.
func (c *Client) GetBlockListDetails(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetBlockListDetails", body)
}

// AddOrUpdateModelBlockRecords creates or updates block-list records.
// LIVE MUTATION.
func (c *Client) AddOrUpdateModelBlockRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateModelBlockRecords", body)
}

// RemoveModelBlockRecords deletes block-list records. LIVE MUTATION.
func (c *Client) RemoveModelBlockRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveModelBlockRecords", body)
}

// BatchDeleteCustomLists deletes custom-list records in bulk. LIVE MUTATION.
func (c *Client) BatchDeleteCustomLists(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/batch-delete-custom-lists", body)
}

// GetTrackingListRecords returns every tracking-list record.
func (c *Client) GetTrackingListRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetTrackingListRecords")
}

// GetTrackingListRecordsFiltered returns filtered tracking-list records. body is
// the freeform filter payload.
func (c *Client) GetTrackingListRecordsFiltered(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetTrackingListRecordsFiltered", body)
}

// AddOrUpdateTrackingListRecords creates or updates tracking-list records.
// LIVE MUTATION.
func (c *Client) AddOrUpdateTrackingListRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateTrackingListRecords", body)
}

// RemoveTrackingListRecords deletes tracking-list records. LIVE MUTATION.
func (c *Client) RemoveTrackingListRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveTrackingListRecords", body)
}
