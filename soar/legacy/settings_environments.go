// LEGACY tier: Siemplify external API (/api/external/v1) ENVIRONMENT settings —
// the tenant's environments (segregation units), their priorities, and load
// balancing. Config-as-code. Reads return RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// GetEnvironments returns the configured environments. body is the freeform
// filter payload.
func (c *Client) GetEnvironments(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetEnvironments", body)
}

// AddOrUpdateEnvironmentRecords creates or updates environments. LIVE MUTATION.
func (c *Client) AddOrUpdateEnvironmentRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateEnvironmentRecords", body)
}

// RemoveEnvironmentRecords deletes environments. LIVE MUTATION.
func (c *Client) RemoveEnvironmentRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveEnvironmentRecords", body)
}

// GetEnvironmentInstanceUrls returns the per-environment instance URLs.
func (c *Client) GetEnvironmentInstanceUrls(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetEnvironmentInstanceUrls")
}

// IsPermittedToEnvironment reports whether the caller may access an environment.
func (c *Client) IsPermittedToEnvironment(ctx context.Context, environmentName string) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/IsPermittedToEnvironment/"+url.PathEscape(environmentName))
}

// GetEnvironmentPriorities returns the environment priority ordering.
func (c *Client) GetEnvironmentPriorities(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/environment-priorities")
}

// GetEnvironmentPriority returns one environment's priority.
func (c *Client) GetEnvironmentPriority(ctx context.Context, environment string) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/environment-priorities/"+url.PathEscape(environment))
}

// UpdateEnvironmentPriority patches one environment's priority. LIVE MUTATION.
func (c *Client) UpdateEnvironmentPriority(ctx context.Context, environment string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPatch, "/settings/environment-priorities/"+url.PathEscape(environment), body)
}

// ResetEnvironmentPriorities resets all environment priorities. LIVE MUTATION.
func (c *Client) ResetEnvironmentPriorities(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/reset-environment-priorities", body)
}

// GetEnvironmentLoadBalancingStatus returns whether load balancing is on.
func (c *Client) GetEnvironmentLoadBalancingStatus(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/environment-load-balancing-status")
}

// ToggleEnvironmentLoadBalancing turns environment load balancing on/off.
// LIVE MUTATION.
func (c *Client) ToggleEnvironmentLoadBalancing(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/toggle-environment-load-balancing", body)
}
