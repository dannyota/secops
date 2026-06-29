// LEGACY tier: the Siemplify external API (/api/external/v1) Settings surface —
// alert-grouping configuration.
//
// Method names are prefixed "SettingX" to stay globally unique across the shared
// *Client; reads return json.RawMessage and writes take a freeform body.
package legacy

import "context"

// GetMetadata returns instance-wide metadata (case stages, users, alert types,
// assignment policy, environment type).
func (c *Client) GetMetadata(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetMetadata")
}

// SettingXGetMaximumAlertsGroupingConfiguration returns the maximum-alerts
// grouping configuration.
func (c *Client) SettingXGetMaximumAlertsGroupingConfiguration(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetMaximumAlertsGroupingConfiguration")
}

// SettingXUpdateAlertGroupingRule updates an alert-grouping rule. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXUpdateAlertGroupingRule(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/UpdateAlertGroupingRule", body)
}
