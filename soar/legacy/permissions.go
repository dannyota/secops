// LEGACY tier: the Siemplify external API (/api/external/v1) Permissions surface.
//
// Permission groups model role-based access in SOAR: each group bundles a set of
// capabilities and, optionally, per-environment status. These endpoints list the
// group cards/types, fetch and duplicate individual groups, and create, update,
// or delete them. This is the reliable external-API path for SOAR access-control
// groups.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"strconv"
)

// PermissionListGroupCards returns basic info for each permission group.
func (c *Client) PermissionListGroupCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/GetPermissionsGroupCards")
}

// PermissionListGroupTypes returns the available permission-group types.
func (c *Client) PermissionListGroupTypes(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/GetPermissionsGroupTypes")
}

// PermissionGetAllEnvironmentStatus returns per-environment status across all
// permission groups.
func (c *Client) PermissionGetAllEnvironmentStatus(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/GetAllEnvironmentStatus")
}

// PermissionGetEnvironmentStatusByGroup returns per-environment status for one
// permission group by its id.
func (c *Client) PermissionGetEnvironmentStatusByGroup(ctx context.Context, permissionGroupID int) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/GetAllEnvironmentStatus/"+strconv.Itoa(permissionGroupID))
}

// PermissionGetGroupTemplateByType returns the permission-group template for a
// given permission type.
func (c *Client) PermissionGetGroupTemplateByType(ctx context.Context, permissionType int) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/GetPermissionsGroupTemplateByType/"+strconv.Itoa(permissionType))
}

// PermissionGet returns the full configuration of one permission group by id.
func (c *Client) PermissionGet(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/permissions/"+strconv.Itoa(id))
}

// PermissionAddOrUpdate creates a new permission group or updates an existing
// one. body is the freeform permission-group payload. LIVE MUTATION.
func (c *Client) PermissionAddOrUpdate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/permissions", body)
}

// PermissionUpdate updates an existing permission group. body is the freeform
// permission-group payload. LIVE MUTATION.
func (c *Client) PermissionUpdate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/permissions", body)
}

// PermissionDuplicate clones an existing permission group by id, returning the
// new group. LIVE MUTATION.
func (c *Client) PermissionDuplicate(ctx context.Context, id int) (RawJSON, error) {
	return c.externalPost(ctx, "/permissions/Duplicate/"+strconv.Itoa(id), nil)
}

// PermissionDelete deletes a permission group by id. LIVE MUTATION; this cannot
// be undone.
func (c *Client) PermissionDelete(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/permissions/"+strconv.Itoa(id), nil)
}
