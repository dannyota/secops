// LEGACY tier: Siemplify external API (/api/external/v1) WEBHOOKS surface —
// inbound webhook endpoints that feed connectors. Reads return RawJSON; writes
// take a freeform body.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// ListWebhookCards returns summary cards for every webhook.
func (c *Client) ListWebhookCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/webhooks-management/Cards")
}

// GetWebhook returns one webhook by identifier.
func (c *Client) GetWebhook(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/webhooks-management/"+url.PathEscape(identifier))
}

// GetWebhookStatistics returns runtime statistics for a webhook.
func (c *Client) GetWebhookStatistics(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/webhooks-management/"+url.PathEscape(identifier)+"/statistics")
}

// GetWebhookLogs returns webhook logs, optionally filtered by webhook identifier
// and minimum log level (both may be empty).
func (c *Client) GetWebhookLogs(ctx context.Context, webhookIdentifier, minimumLogLevel string) (RawJSON, error) {
	q := url.Values{}
	if webhookIdentifier != "" {
		q.Set("WebhookIdentifier", webhookIdentifier)
	}
	if minimumLogLevel != "" {
		q.Set("MinimumLogLevel", minimumLogLevel)
	}
	return c.externalGetQuery(ctx, "/webhooks-management/Logs", q)
}

// ExportWebhookLogs exports a webhook's logs.
func (c *Client) ExportWebhookLogs(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/webhooks-management/ExportLogs/"+url.PathEscape(identifier))
}

// CreateWebhook creates a webhook. body is the freeform webhook payload. LIVE
// MUTATION.
func (c *Client) CreateWebhook(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/webhooks-management", body)
}

// UpdateWebhook updates a webhook. LIVE MUTATION.
func (c *Client) UpdateWebhook(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/webhooks-management", body)
}

// RevokeWebhook revokes a webhook's key by identifier. LIVE MUTATION.
func (c *Client) RevokeWebhook(ctx context.Context, identifier string, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/webhooks-management/Revoke/"+url.PathEscape(identifier), body)
}

// DeleteWebhook deletes a webhook by identifier. LIVE MUTATION; cannot be undone.
func (c *Client) DeleteWebhook(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/webhooks-management/"+url.PathEscape(identifier), nil)
}
