package legacy

import "context"

// AuditGetData returns audit log entries. body carries paging/filter params.
// The legacy external-API path for this is undocumented in the swagger; use the
// v1alpha legacySoarAudit surface (soar.Client.AuditGetData) instead.
func (c *Client) AuditGetData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetAuditGeneralData", body)
}
