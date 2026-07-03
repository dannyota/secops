// data_surfaces.go — MODERN v1alpha coverage for the SOAR data / enrichment
// surfaces that also run on the legacy reconcile engine (SLA definitions,
// networks, entity block-lists) plus read access to ontology records and remote
// agents. All answer on the SOAR host (siemplify-soar, AppKey, v1alpha) — see
// soar/v1alpha_probe_test.go. Shapes vary per surface, so records are passed and
// returned raw. The create/get/update/delete primitives are the shared collection
// helpers in config_surfaces.go.
//
// Write status: create→get→delete is live-validated for slaDefinitions and
// soarNetworks (they do NOT 500). entitiesBlocklists writes reach the endpoint
// (400 validation, not 500) but its `action` (ActionScope) and `entityType` enums
// are undocumented — the create body must carry server-valid tokens, so the write
// is wired but not yet validated. Reads are confirmed for all.

package soar

import (
	"context"
	"encoding/json"
)

// SlaDefinition writes/reads (modern v1alpha). The body is an SlaDefinition: it
// uses STRING enums (SlaType, SlaTimeUnit, AlertType) — not the legacy integer
// codings. slaType is immutable after create; environments[] must be sent as []
// (never null). LIVE MUTATIONS for the write methods.
func (c *Client) ListSlaDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "slaDefinitions")
}

// CreateSlaDefinition creates an SLA definition (v1alpha slaDefinitions).
func (c *Client) CreateSlaDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "slaDefinitions", body)
}

// GetSlaDefinition returns a single SLA definition by id (v1alpha slaDefinitions).
func (c *Client) GetSlaDefinition(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "slaDefinitions", id)
}

// UpdateSlaDefinition patches an SLA definition (v1alpha slaDefinitions).
func (c *Client) UpdateSlaDefinition(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "slaDefinitions", id, body, updateMask...)
}

// DeleteSlaDefinition deletes an SLA definition by id (v1alpha slaDefinitions).
func (c *Client) DeleteSlaDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "slaDefinitions", id)
}

// SoarNetwork writes/reads (modern v1alpha). The body is a SoarNetwork:
// displayName + address (CIDR) + environmentsJson (a JSON-encoded string, not a
// repeated field) + priority. LIVE MUTATIONS for the write methods.
func (c *Client) ListSoarNetworks(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "soarNetworks")
}

// CreateSoarNetwork creates a SOAR network (v1alpha soarNetworks).
func (c *Client) CreateSoarNetwork(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "soarNetworks", body)
}

// GetSoarNetwork returns a single SOAR network by id (v1alpha soarNetworks).
func (c *Client) GetSoarNetwork(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "soarNetworks", id)
}

// UpdateSoarNetwork patches a SOAR network (v1alpha soarNetworks).
func (c *Client) UpdateSoarNetwork(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "soarNetworks", id, body, updateMask...)
}

// DeleteSoarNetwork deletes a SOAR network by id (v1alpha soarNetworks).
func (c *Client) DeleteSoarNetwork(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "soarNetworks", id)
}

// EntitiesBlocklist writes/reads (modern v1alpha). The body is an
// EntitiesBlocklist: entityIdentifier + entityType + action + environmentsJson.
// NOTE: `action` is the server enum ActionScope and `entityType` is also enum-
// validated, but neither value set is documented — supply a server-valid token.
// Reads are confirmed; create reaches the endpoint (400 on a bad enum, not 500).
// LIVE MUTATIONS for the write methods.
func (c *Client) ListEntitiesBlocklists(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "entitiesBlocklists")
}

// CreateEntitiesBlocklist creates an entity block-list entry (v1alpha entitiesBlocklists).
func (c *Client) CreateEntitiesBlocklist(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "entitiesBlocklists", body)
}

// GetEntitiesBlocklist returns a single entity block-list entry by id (v1alpha entitiesBlocklists).
func (c *Client) GetEntitiesBlocklist(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "entitiesBlocklists", id)
}

// DeleteEntitiesBlocklist deletes an entity block-list entry by id (v1alpha entitiesBlocklists).
func (c *Client) DeleteEntitiesBlocklist(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "entitiesBlocklists", id)
}

// ListOntologyRecords returns the ontology records (modern v1alpha, SOAR host).
// Read-only here; the write path is import/export (ZIP) + the visualFamilies and
// mappingRules sub-resources, not a plain create.
func (c *Client) ListOntologyRecords(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "ontologyRecords")
}

// ListRemoteAgents returns the SOAR remote agents (modern v1alpha). Read-only.
func (c *Client) ListRemoteAgents(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "remoteAgents")
}
