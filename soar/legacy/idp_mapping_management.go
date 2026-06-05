// LEGACY tier: the Siemplify external API (/api/external/v1) IdpMappingManagement
// surface. These endpoints manage the mapping between an external identity
// provider's groups and SOAR. Reads return json.RawMessage and writes take a
// freeform body (the caller supplies/decodes only the fields it needs). All
// methods speak the AppKey-authenticated external API via c.t.External.
package legacy

import (
	"context"
	"net/url"
)

// IdpMappingList returns the configured IdP group mappings for an external
// identity provider. externalProviderName selects the provider (e.g. "SecOps");
// pass "" to use the server default.
func (c *Client) IdpMappingList(ctx context.Context, externalProviderName string) (RawJSON, error) {
	q := url.Values{}
	if externalProviderName != "" {
		q.Set("externalProviderName", externalProviderName)
	}
	return c.externalGetQuery(ctx, "/idpMapping", q)
}

// IdpMappingCreate creates an IdP group mapping. body is the freeform
// CreateManagementIdpGroupMapperDto payload. LIVE MUTATION.
func (c *Client) IdpMappingCreate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/idpMapping", body)
}
