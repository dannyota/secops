// LEGACY tier: the Siemplify external API (/api/external/v1) integrations surface.
//
// Integrations are the installed marketplace packs; an integration *instance* is
// a configured copy (credentials, params) scoped to an environment. These
// endpoints list installed integrations and manage instances. Reads return
// RawJSON; writes take a freeform body. AppKey auth.
package legacy

import (
	"context"
	"net/url"
)

// ListInstalledIntegrations returns every installed integration in the platform.
func (c *Client) ListInstalledIntegrations(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/integrations/GetInstalledIntegrations")
}

// GetIntegrationDefaultInstance returns the default instance of an integration,
// keyed by the integration identifier.
func (c *Client) GetIntegrationDefaultInstance(ctx context.Context, integrationIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/integrations/GetIntegrationDefaultInstance/"+url.PathEscape(integrationIdentifier))
}

// GetIntegrationInstanceSettings returns the settings of one integration instance.
func (c *Client) GetIntegrationInstanceSettings(ctx context.Context, integrationInstanceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/integrations/GetIntegrationInstanceSettings/"+url.PathEscape(integrationInstanceID))
}

// GetPlaybooksUsingInstance returns the names of playbooks that use a specific
// integration instance (useful before changing or deleting it).
func (c *Client) GetPlaybooksUsingInstance(ctx context.Context, integrationInstanceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/integrations/GetPlaybooksNamesUsingIntegrationInstance/"+url.PathEscape(integrationInstanceID))
}

// ListOptionalIntegrationInstances returns the instances available for a specific
// integration. body is the freeform selector payload.
func (c *Client) ListOptionalIntegrationInstances(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/integrations/GetOptionalIntegrationInstances", body)
}

// ListEnvironmentCards returns the environments as summary cards. body is the
// freeform filter payload.
func (c *Client) ListEnvironmentCards(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/integrations/GetAllEnvironmentCards", body)
}

// ListEnvironmentInstalledIntegrations returns the installed integrations per
// environment. body is the freeform filter payload.
func (c *Client) ListEnvironmentInstalledIntegrations(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/integrations/GetEnvironmentInstalledIntegrations", body)
}

// CreateIntegrationInstance creates a new integration instance. body is the
// freeform instance payload. LIVE MUTATION.
func (c *Client) CreateIntegrationInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/integrations/CreateIntegrationInstance", body)
}

// DeleteIntegrationInstance deletes an integration instance. body carries the
// instance id. LIVE MUTATION; this cannot be undone.
func (c *Client) DeleteIntegrationInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/integrations/DeleteIntegrationInstance", body)
}
