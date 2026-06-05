// LEGACY tier: the Siemplify external API (/api/external/v1) Federation surface.
//
// Federation links a SOAR instance into a multi-platform topology: it enumerates
// the federated platforms and surfaces their cases through the local instance.
// These endpoints predate the modern federation model and are kept here until it
// covers them.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// FederationListCases returns federated cases, paginated. continuationToken is
// the opaque cursor from a prior page (empty for the first page); pageSize caps
// the number of cases returned (<=0 omits it, letting the server default apply).
func (c *Client) FederationListCases(ctx context.Context, continuationToken string, pageSize int) (RawJSON, error) {
	q := url.Values{}
	if continuationToken != "" {
		q.Set("continuationToken", continuationToken)
	}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	return c.externalGetQuery(ctx, "/federation/cases", q)
}

// FederationBatchPatchCases applies a batch of partial updates to federated
// cases. body is the freeform legacy patch payload. LIVE MUTATION.
func (c *Client) FederationBatchPatchCases(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPatch, "/federation/cases/batch-patch", body)
}

// FederationListPlatforms returns the platforms federated with this instance.
func (c *Client) FederationListPlatforms(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/federation/platforms")
}

// FederationDeletePlatform removes a federated platform by its numeric id.
// LIVE MUTATION; this cannot be undone.
func (c *Client) FederationDeletePlatform(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/federation/platforms/"+strconv.Itoa(id), nil)
}
