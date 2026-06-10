package soar

import (
	"context"
	"encoding/json"
)

// AuditGetData returns SOAR audit log entries via the v1alpha surface.
func (c *Client) AuditGetData(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacySoarAudit:legacyGetAuditDataV2", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
