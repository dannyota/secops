package legacy

import "context"

// SystemGetVersion returns the SOAR platform version.
func (c *Client) SystemGetVersion(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetSystemVersion")
}

// SystemGetLicenseStatus returns the SOAR license status.
func (c *Client) SystemGetLicenseStatus(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetLicenseStatus")
}

// SystemGetMaxDataRetention returns the maximum data retention period (months).
func (c *Client) SystemGetMaxDataRetention(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetMaximumDataRetentionValue")
}
