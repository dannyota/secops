// LEGACY tier: the Siemplify external API (/api/external/v1) PubSubBackfill
// surface. PubSub backfill re-publishes historical data into the ingestion
// pipeline for a tenant, used to replay or recover events.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/url"
)

// PubSubBackfillTrigger starts a PubSub backfill for the given tenant. body is
// the freeform PubSubBackfillRequest payload. LIVE MUTATION.
func (c *Client) PubSubBackfillTrigger(ctx context.Context, tenantID string, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/"+url.PathEscape(tenantID)+"/pubsub-backfill:trigger", body)
}
