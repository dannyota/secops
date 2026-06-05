// LEGACY tier: the Siemplify external API (/api/external/v1) SOC Roles surface.
//
// SOC roles group SOAR users by responsibility (e.g. Tier 1, Tier 2) and drive
// case routing, queue ownership, and permission scoping. These endpoints list,
// fetch, upsert, and delete role records and check whether a role still has
// users assigned before removal.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"strconv"
)

// SocRoleList returns all SOC role records in the tenant.
func (c *Client) SocRoleList(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/socroles/GetSocRoles")
}

// SocRoleGet returns one SOC role record by its numeric id.
func (c *Client) SocRoleGet(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/socroles/GetSocRole/"+strconv.Itoa(id))
}

// SocRoleListByEnvironments returns SOC role records scoped to the environments
// named in body (a freeform legacy payload).
func (c *Client) SocRoleListByEnvironments(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/socroles/GetSocRolesByEnvironments", body)
}

// SocRoleHasAssignedUsers reports whether the SOC role with the given numeric id
// still has users assigned (check before deletion).
func (c *Client) SocRoleHasAssignedUsers(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/socroles/HasAssignedUsers/"+strconv.Itoa(id))
}

// SocRoleAddOrUpdate creates a new SOC role or updates an existing one. body is
// the freeform SOC role payload. LIVE MUTATION.
func (c *Client) SocRoleAddOrUpdate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/socroles/AddOrUpdateSocRole", body)
}

// SocRoleDelete deletes a SOC role. body selects the role to remove (freeform
// legacy payload). LIVE MUTATION; this cannot be undone.
func (c *Client) SocRoleDelete(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/socroles/DeleteSocRole", body)
}
