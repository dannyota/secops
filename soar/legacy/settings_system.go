// LEGACY tier: Siemplify external API (/api/external/v1) SYSTEM settings —
// version, proxy, certificate, event entity types, and case-routing policies.
// Reads return RawJSON; writes take a freeform body.
package legacy

import "context"

// GetSystemEventEntityTypes returns the system's event entity types.
func (c *Client) GetSystemEventEntityTypes(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetSystemEventEntityTypes")
}

// GetPublicCertificate returns the platform's public certificate.
func (c *Client) GetPublicCertificate(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetPublicCertificate")
}

// GetProxySettings returns the outbound proxy configuration.
func (c *Client) GetProxySettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetProxySettings")
}

// TestProxySettings tests a proxy configuration. body carries the proxy details.
func (c *Client) TestProxySettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/TestProxySettings", body)
}

// GetCaseAssignmentPolicySettings returns the case auto-assignment policy.
func (c *Client) GetCaseAssignmentPolicySettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetCaseAssigmentPolicySettings")
}

// AddOrUpdateCaseAssignmentPolicySettings sets the case auto-assignment policy.
// LIVE MUTATION.
func (c *Client) AddOrUpdateCaseAssignmentPolicySettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateCaseAssignmentPolicySettings", body)
}

// GetMoveCaseBetweenEnvironmentsPolicySettings returns the cross-environment
// case-move policy.
func (c *Client) GetMoveCaseBetweenEnvironmentsPolicySettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetMoveCaseBetweenEnvironmentsPolicySettings")
}

// AddOrUpdateMoveCaseBetweenEnvironmentsPolicySettings sets the cross-environment
// case-move policy. LIVE MUTATION.
func (c *Client) AddOrUpdateMoveCaseBetweenEnvironmentsPolicySettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateMoveCaseBetweenEnvironmentsPolicySettings", body)
}
