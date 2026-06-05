// Shared helpers for the LEGACY Siemplify external API (/api/external/v1).
//
// The external surface is broad (hundreds of operations) but uniform: AppKey
// auth, JSON in/out, and deeply-nested, schema-unstable payloads. Rather than
// model every shape, the typed methods in this package are thin wrappers that
// return RawJSON (an undecoded body) for reads and take a freeform body for
// writes — the caller decodes only the fields it needs. These helpers centralize
// the c.t.External plumbing so each endpoint method stays one line.
//
// (The external API is almost entirely POST-with-body; query parameters are
// rare, so the helpers omit them — add a query-aware variant if an endpoint
// needs one.)
package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

// RawJSON is an undecoded legacy response body.
type RawJSON = json.RawMessage

// externalGet issues a GET to an external-API path and returns the raw body.
func (c *Client) externalGet(ctx context.Context, path string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodGet, path, nil)
}

// externalGetQuery issues a GET with URL query parameters.
func (c *Client) externalGetQuery(ctx context.Context, path string, q url.Values) (RawJSON, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodGet, path, nil, &out, transport.Query(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// externalPost issues a POST with a JSON body to an external-API path.
func (c *Client) externalPost(ctx context.Context, path string, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPost, path, body)
}

// externalDo issues method+path (+optional body) and returns the raw response.
func (c *Client) externalDo(ctx context.Context, method, path string, body any) (RawJSON, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
