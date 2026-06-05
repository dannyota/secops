// LEGACY tier: Siemplify external API (/api/external/v1) IDENTITY settings —
// IdP group-to-role mappings and external authentication providers. These are
// REST-style resources (list/get/create/update/delete). Config-as-code (no
// secrets in the read responses beyond what the server returns). Reads return
// RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// --- IdP group mapping ---

// ListIdpGroupMappings returns every IdP group-to-role mapping.
func (c *Client) ListIdpGroupMappings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/idp-group-mapping")
}

// GetIdpGroupMappingCount returns the number of IdP group mappings.
func (c *Client) GetIdpGroupMappingCount(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/idp-group-mapping/count")
}

// GetIdpGroupMapping returns one IdP group mapping by id.
func (c *Client) GetIdpGroupMapping(ctx context.Context, id string) (RawJSON, error) {
	return c.externalGet(ctx, "/idp-group-mapping/"+url.PathEscape(id))
}

// CreateIdpGroupMapping creates an IdP group mapping. LIVE MUTATION.
func (c *Client) CreateIdpGroupMapping(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/idp-group-mapping", body)
}

// UpdateIdpGroupMapping updates an IdP group mapping by id. LIVE MUTATION.
func (c *Client) UpdateIdpGroupMapping(ctx context.Context, id string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/idp-group-mapping/"+url.PathEscape(id), body)
}

// DeleteIdpGroupMapping deletes an IdP group mapping by id. LIVE MUTATION.
func (c *Client) DeleteIdpGroupMapping(ctx context.Context, id string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/idp-group-mapping/"+url.PathEscape(id), nil)
}

// --- External authentication settings ---

// ListExternalAuthSettings returns every external authentication provider.
func (c *Client) ListExternalAuthSettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/external-authentication-settings")
}

// GetExternalAuthSetting returns one external auth provider by id.
func (c *Client) GetExternalAuthSetting(ctx context.Context, id string) (RawJSON, error) {
	return c.externalGet(ctx, "/external-authentication-settings/"+url.PathEscape(id))
}

// CreateExternalAuthSetting creates an external auth provider. LIVE MUTATION.
func (c *Client) CreateExternalAuthSetting(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/external-authentication-settings", body)
}

// UpdateExternalAuthSetting replaces an external auth provider by id. LIVE MUTATION.
func (c *Client) UpdateExternalAuthSetting(ctx context.Context, id string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/external-authentication-settings/"+url.PathEscape(id), body)
}

// PatchExternalAuthSetting partially updates an external auth provider by id.
// LIVE MUTATION.
func (c *Client) PatchExternalAuthSetting(ctx context.Context, id string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPatch, "/external-authentication-settings/"+url.PathEscape(id), body)
}

// DeleteExternalAuthSetting deletes an external auth provider by id. LIVE MUTATION.
func (c *Client) DeleteExternalAuthSetting(ctx context.Context, id string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/external-authentication-settings/"+url.PathEscape(id), nil)
}
