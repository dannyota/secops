// MODERN. Connector instance configuration on the v1alpha SOAR surface.
//
// A connector instance is a configured, schedulable poller that pulls events
// into SecOps. It lives under an integration's connector definition at
// integrations/{i}/connectors/{c}/connectorInstances/{id}. This file exposes
// read/update of that config plus a fetch of the connector's latest schema
// definition.

package soar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

// ConnectorParameter is one connector-instance parameter. The live v1alpha
// payload returns parameters as an array of these descriptors; Key() addresses a
// parameter by DisplayName (live) or Name (older shape). Value is always a string
// (secrets read back masked).
type ConnectorParameter struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Value       string `json:"value"`
	Type        string `json:"type,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Mandatory   bool   `json:"mandatory,omitempty"`
	Advanced    bool   `json:"advanced,omitempty"`
}

// Key returns the parameter's addressing key: DisplayName when set, else Name.
func (p ConnectorParameter) Key() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// ConnectorInstance is a configured connector poller. Parameters is the ordered
// list of parameter descriptors; Raw preserves the full server payload (schema
// metadata, statistics) the typed fields omit.
type ConnectorInstance struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	// Enabled/IntervalSeconds carry no omitempty: a sparse PATCH that sets
	// enabled=false or intervalSeconds=0 must serialize the zero value, or the
	// server (which reads the body field-by-field per updateMask) silently
	// no-ops the change.
	Enabled          bool                 `json:"enabled"`
	IntervalSeconds  int                  `json:"intervalSeconds"`
	Parameters       []ConnectorParameter `json:"-"` // decoded tolerantly (array or older map)
	AllowList        []string             `json:"allowList,omitempty"`
	ProductFieldName string               `json:"productFieldName,omitempty"`
	EventFieldName   string               `json:"eventFieldName,omitempty"`
	Raw              json.RawMessage      `json:"-"`
}

// UnmarshalJSON decodes the typed fields, parses parameters tolerant of both the
// live array-of-descriptors and an older flat {name:value} map, and retains the
// full payload in Raw.
func (ci *ConnectorInstance) UnmarshalJSON(data []byte) error {
	type alias ConnectorInstance
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*ci = ConnectorInstance(a)
	params, err := decodeConnectorParams(data)
	if err != nil {
		return err
	}
	ci.Parameters = params
	ci.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// decodeConnectorParams reads the "parameters" field tolerant of both shapes: the
// live array of descriptor objects, and an older flat {name:value} map. A present
// but unparseable value is an error, not a silently-empty parameter set (which
// would drop the connector's config on the floor).
func decodeConnectorParams(data []byte) ([]ConnectorParameter, error) {
	var holder struct {
		Params json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal(data, &holder); err != nil {
		return nil, err
	}
	t := bytes.TrimSpace(holder.Params)
	// Absent or explicit JSON null both mean "no parameters" — tolerate them so a
	// single such record doesn't fail the whole list decode. Only a present,
	// structured-but-unexpected value is an error.
	if len(t) == 0 || string(t) == "null" {
		return nil, nil
	}
	switch t[0] {
	case '[':
		var arr []ConnectorParameter
		if err := json.Unmarshal(t, &arr); err != nil {
			return nil, fmt.Errorf("connector parameters (array form): %w", err)
		}
		return arr, nil
	case '{':
		var m map[string]string
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, fmt.Errorf("connector parameters (map form): %w", err)
		}
		out := make([]ConnectorParameter, 0, len(m))
		for k, v := range m {
			out = append(out, ConnectorParameter{Name: k, Value: v})
		}
		return out, nil
	}
	return nil, fmt.Errorf("connector parameters: unexpected JSON %s", string(t))
}

func connectorInstancePath(integration, connectorID, instanceID string) string {
	return fmt.Sprintf("integrations/%s/connectors/%s/connectorInstances/%s",
		integration, connectorID, instanceID)
}

// ListConnectorInstances returns every configured instance of a connector
// (Google-style {items,nextPageToken} pagination).
func (c *Client) ListConnectorInstances(ctx context.Context, integration, connectorID string) ([]ConnectorInstance, error) {
	base := fmt.Sprintf("integrations/%s/connectors/%s/connectorInstances", integration, connectorID)
	var all []ConnectorInstance
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{}
		if token != "" {
			q.Set("pageToken", token)
		}
		// The v1alpha LIST collection key is reported either as the resource name
		// or the generic "items"; accept both so a staging tenant on either shape
		// pulls correctly.
		var resp struct {
			ConnectorInstances []ConnectorInstance `json:"connectorInstances"`
			Items              []ConnectorInstance `json:"items"`
			NextPageToken      string              `json:"nextPageToken"`
		}
		if err := c.t.V1Alpha(ctx, "GET", base, nil, &resp, transport.Query(q)); err != nil {
			return "", err
		}
		batch := resp.ConnectorInstances
		if len(batch) == 0 {
			batch = resp.Items
		}
		all = append(all, batch...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetConnectorInstance reads a connector instance's configuration.
func (c *Client) GetConnectorInstance(ctx context.Context, integration, connectorID, instanceID string) (*ConnectorInstance, error) {
	var out ConnectorInstance
	if err := c.t.V1Alpha(ctx, "GET", connectorInstancePath(integration, connectorID, instanceID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateConnectorInstance patches a connector instance. Pass updateMask to scope
// the sparse update to specific fields (e.g. "enabled", "parameters").
//
// DEVIATION: secret parameters read back masked from GetConnectorInstance (a
// "***…" sentinel rather than the real value). The server treats that sentinel
// as "unchanged", so a round-trip get→patch is safe: pass the masked value back
// verbatim to leave the secret intact. Only send a real cleartext value when you
// genuinely intend to rotate it — and never log or commit that value.
func (c *Client) UpdateConnectorInstance(ctx context.Context, integration, connectorID, instanceID string, body any, updateMask ...string) (*ConnectorInstance, error) {
	var out ConnectorInstance
	var opts []transport.Option
	if len(updateMask) > 0 {
		opts = append(opts, transport.UpdateMask(updateMask...))
	}
	if err := c.t.V1Alpha(ctx, "PATCH", connectorInstancePath(integration, connectorID, instanceID), body, &out, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchLatestConnectorDefinition returns the connector's current schema
// definition (parameter descriptors, defaults) as raw JSON, for reconciling a
// stored instance against the latest connector version.
func (c *Client) FetchLatestConnectorDefinition(ctx context.Context, integration, connectorID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	resource := connectorInstancePath(integration, connectorID, instanceID) + ":fetchLatestDefinition"
	if err := c.t.V1Alpha(ctx, "GET", resource, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunConnectorInstanceOnDemand triggers a configured connector to poll now,
// rather than waiting for its schedule — the operational complement to the
// list/get/patch instance ops (e.g. to validate a connector change after a push).
// LIVE MUTATION. (Modern SOAR v1alpha — may 500 intermittently.)
func (c *Client) RunConnectorInstanceOnDemand(ctx context.Context, integration, connectorID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	resource := connectorInstancePath(integration, connectorID, instanceID) + ":runOnDemand"
	if err := c.t.V1Alpha(ctx, "POST", resource, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
