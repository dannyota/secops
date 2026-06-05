// LEGACY tier: the Siemplify external API (/api/external/v1) connector surface.
//
// Connectors are the ingestion sources that feed alerts into SOAR. These
// endpoints manage connector *instances* (configured connectors) and expose the
// connector *definitions* (templates) available in the tenant. They predate the
// modern v1alpha connector model and are kept here until it covers them.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// ListConnectorCards returns basic info for each accessible connector instance.
func (c *Client) ListConnectorCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/connectors/cards")
}

// ListConnectorTemplateCards returns basic info for each connector *definition*
// (template) available to configure instances from.
func (c *Client) ListConnectorTemplateCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/connectors/template-cards")
}

// GetConnector returns the full configuration of one connector instance by its
// identifier (the connector instance id, a string).
func (c *Client) GetConnector(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/connectors/"+url.PathEscape(identifier))
}

// GetConnectorStatistics returns runtime statistics for one connector instance.
func (c *Client) GetConnectorStatistics(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/connectors/"+url.PathEscape(identifier)+"/statistics")
}

// SaveConnector adds a new connector instance or updates an existing one. body
// is the freeform connector-instance payload. LIVE MUTATION.
func (c *Client) SaveConnector(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/connectors", body)
}

// DeleteConnector deletes a connector instance by identifier. LIVE MUTATION;
// this cannot be undone.
func (c *Client) DeleteConnector(ctx context.Context, identifier string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/connectors/"+url.PathEscape(identifier), nil)
}

// GetConnectorTemplate returns detailed info for a specific connector definition.
// body selects the template (freeform legacy payload).
func (c *Client) GetConnectorTemplate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/connectors/template", body)
}

// FetchConnectorSampleData executes a connector once for testing and returns the
// sample it produced. body is the freeform connector payload. This RUNS the
// connector against its live source — use only on connectors you intend to test.
func (c *Client) FetchConnectorSampleData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/connectors/fetch-sample-data", body)
}

// UpdateConnectorFromIDE updates a connector instance to its definition's latest
// version. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) UpdateConnectorFromIDE(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/connectors/update-from-ide", body)
}
