// LEGACY tier: the Siemplify external API (/api/external/v1) Settings surface —
// advanced-reports configuration.
//
// Method names are prefixed "SettingX" to stay globally unique across the shared
// *Client; reads return json.RawMessage and writes take a freeform body.
package legacy

import "context"

// SettingXGetAdvancedReportsSettings returns the advanced-reports configuration.
func (c *Client) SettingXGetAdvancedReportsSettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetAdvancedReportsSettings")
}

// SettingXSaveAdvancedReportsSettings persists the advanced-reports
// configuration. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXSaveAdvancedReportsSettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/SaveAdvancedReportsSettings", body)
}

// SettingXTestAdvancedReportsSettings tests connectivity to the configured
// advanced-reports backend.
func (c *Client) SettingXTestAdvancedReportsSettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/TestAdvancedReportsSettings")
}
