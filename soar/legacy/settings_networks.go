// LEGACY tier: Siemplify external API (/api/external/v1) NETWORK settings — the
// named networks/CIDRs used for entity enrichment and internal/external scoping.
// Config-as-code. Reads return RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// GetNetworkDetails returns the configured network records. body is the freeform
// filter payload.
func (c *Client) GetNetworkDetails(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetNetworkDetails", body)
}

// AddOrUpdateNetworkDetailsRecords creates or updates network records.
// LIVE MUTATION.
func (c *Client) AddOrUpdateNetworkDetailsRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateNetworkDetailsRecords", body)
}

// RemoveNetworkDetailsRecords deletes specific network records. LIVE MUTATION.
func (c *Client) RemoveNetworkDetailsRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveNetworkDetailsRecords", body)
}

// RemoveAllNetworkDetailsRecords deletes every network record. LIVE MUTATION;
// high blast radius.
func (c *Client) RemoveAllNetworkDetailsRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveAllNetworkDetailsRecords", body)
}

// DeleteNetwork deletes one network by identifier. LIVE MUTATION.
func (c *Client) DeleteNetwork(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/settings/networks/"+url.PathEscape(identifier), nil)
}

// DeletePermittedNetworks deletes the permitted-networks set. LIVE MUTATION.
func (c *Client) DeletePermittedNetworks(ctx context.Context) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/settings/networks/permitted", nil)
}
