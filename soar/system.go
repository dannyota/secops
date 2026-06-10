package soar

import (
	"context"
	"encoding/json"
)

// SystemGetVersion returns the SOAR platform version via the v1alpha surface.
func (c *Client) SystemGetVersion(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacySystem:legacyGetSystemVersion", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SystemGetLicenseStatus returns the SOAR license status via the v1alpha surface.
func (c *Client) SystemGetLicenseStatus(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacySystem:legacyGetLicenseStatus", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SystemGetMaxDataRetention returns the max data retention period via the v1alpha surface.
func (c *Client) SystemGetMaxDataRetention(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacySystem:legacyGetMaximumDataRetentionValue", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
