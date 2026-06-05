// LEGACY tier: the Siemplify external API (/api/external/v1)
// ConfigurationItemsManagement surface.
//
// Configuration items are per-tenant key/value settings, optionally grouped by
// category. These endpoints read the items for a tenant and replace or batch
// patch them. Shapes are the deeply-nested, schema-unstable legacy payloads, so
// reads return json.RawMessage and writes take a freeform body (the caller
// supplies/decodes only the fields it needs). All methods speak the
// AppKey-authenticated external API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// ConfigItemList returns the configuration items for one tenant. key and
// category are optional filters; pass "" to omit either.
func (c *Client) ConfigItemList(ctx context.Context, tenantID, key, category string) (RawJSON, error) {
	path := "/configuration-items/" + url.PathEscape(tenantID)
	q := url.Values{}
	if key != "" {
		q.Set("key", key)
	}
	if category != "" {
		q.Set("category", category)
	}
	if len(q) == 0 {
		return c.externalGet(ctx, path)
	}
	return c.externalGetQuery(ctx, path, q)
}

// ConfigItemUpdate replaces the configuration items for one tenant. body is the
// freeform configuration-items payload. LIVE MUTATION.
func (c *Client) ConfigItemUpdate(ctx context.Context, tenantID string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/configuration-items/"+url.PathEscape(tenantID), body)
}

// ConfigItemBatchUpdate applies a batch of changes to a tenant's configuration
// items. body is the freeform batch payload. LIVE MUTATION.
func (c *Client) ConfigItemBatchUpdate(ctx context.Context, tenantID string, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/"+url.PathEscape(tenantID)+"/configuration-items:batchUpdate", body)
}
