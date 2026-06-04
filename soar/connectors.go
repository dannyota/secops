// MODERN. Connector instance configuration on the v1alpha SOAR surface.
//
// A connector instance is a configured, schedulable poller that pulls events
// into SecOps. It lives under an integration's connector definition at
// integrations/{i}/connectors/{c}/connectorInstances/{id}. This file exposes
// read/update of that config plus a fetch of the connector's latest schema
// definition.

package soar

import (
	"context"
	"encoding/json"
	"fmt"

	"danny.vn/secops/soar/internal/transport"
)

// ConnectorInstance is a configured connector poller. Parameters is the flat
// settings bag SOAR returns entirely as strings; Raw preserves the full server
// payload (schema metadata, per-parameter descriptors) the typed fields omit.
type ConnectorInstance struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	// Enabled/IntervalSeconds carry no omitempty: a sparse PATCH that sets
	// enabled=false or intervalSeconds=0 must serialize the zero value, or the
	// server (which reads the body field-by-field per updateMask) silently
	// no-ops the change.
	Enabled          bool              `json:"enabled"`
	IntervalSeconds  int               `json:"intervalSeconds"`
	Parameters       map[string]string `json:"parameters,omitempty"`
	AllowList        []string          `json:"allowList,omitempty"`
	ProductFieldName string            `json:"productFieldName,omitempty"`
	EventFieldName   string            `json:"eventFieldName,omitempty"`
	Raw              json.RawMessage   `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (ci *ConnectorInstance) UnmarshalJSON(data []byte) error {
	type alias ConnectorInstance
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*ci = ConnectorInstance(a)
	ci.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func connectorInstancePath(integration, connectorID, instanceID string) string {
	return fmt.Sprintf("integrations/%s/connectors/%s/connectorInstances/%s",
		integration, connectorID, instanceID)
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
