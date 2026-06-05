// LEGACY tier: the Siemplify external API (/api/external/v1) EntitySearch surface.
//
// These endpoints query the entity index built from ingested cases/alerts,
// returning a paged set of entities (and a total count) that match a freeform
// search-request payload. Like the rest of the legacy surface, the request and
// response shapes are deeply nested and schema-unstable, so reads return
// json.RawMessage and writes take a freeform body — the caller decodes only the
// fields it needs. All methods speak the AppKey-authenticated external API via
// c.t.External.
package legacy

import "context"

// EntitySearchCount returns the total number of entities matching the search
// request. body is the freeform entity-search request payload.
func (c *Client) EntitySearchCount(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/entity-search/count", body)
}

// EntitySearchListEntities returns a page of entities matching the search
// request. body is the freeform entity-search request payload (filters,
// pagination, etc.).
func (c *Client) EntitySearchListEntities(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/entity-search/entities", body)
}
