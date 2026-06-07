package soar

import (
	"context"
	"encoding/json"
)

// IdP mapping groups (legacySoarIdpMappingGroups) map an external IdP group to
// SOAR environments, SOC roles, and permission groups. The official docs file
// this resource under the chronicle instance path, but it 500s on the chronicle
// host and answers on the SOAR host (siemplify-soar, AppKey, v1alpha) — so it
// lives here on the SOAR plane. WRITES TOUCH LIVE ACCESS (who can do what) —
// operate with care. Records are returned/sent as raw JSON (rich, nested shape).
//
// The resource id is server-assigned on create.
const idpMappingGroups = "legacySoarIdpMappingGroups"

// ListIdpMappingGroups returns every IdP mapping group as raw JSON.
func (c *Client) ListIdpMappingGroups(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, idpMappingGroups)
}

// GetIdpMappingGroup fetches one IdP mapping group by id, as raw JSON.
func (c *Client) GetIdpMappingGroup(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, idpMappingGroups, id)
}

// CreateIdpMappingGroup creates a mapping group from the freeform body and returns
// the server echo. LIVE ACCESS MUTATION.
func (c *Client) CreateIdpMappingGroup(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, idpMappingGroups, body)
}

// UpdateIdpMappingGroup PATCHes a mapping group; pass updateMask to scope the
// write. LIVE ACCESS MUTATION.
func (c *Client) UpdateIdpMappingGroup(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, idpMappingGroups, id, body, updateMask...)
}

// DeleteIdpMappingGroup deletes a mapping group by id. LIVE ACCESS MUTATION.
func (c *Client) DeleteIdpMappingGroup(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, idpMappingGroups, id)
}

// GetIdpExternalProviders returns the external identity providers configured for
// the system (read), as raw JSON.
func (c *Client) GetIdpExternalProviders(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", idpMappingGroups+":getExternalProviders", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
