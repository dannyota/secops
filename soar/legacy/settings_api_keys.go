// LEGACY tier: the Siemplify external API (/api/external/v1) Settings surface —
// API-key administration.
//
// API keys are the SOAR external-API credentials (the AppKey family), scoped by
// permission group / SOC role / environment. The list endpoint
// (GET /settings/GetApiKeys) is **metadata only**: the server returns the secret
// `key` field already masked (****), so there is no secret to leak on read — and
// this SDK drops it from the typed view entirely (read paths never surface a key,
// masked or not; House Rule 4). The endpoint is GET (a POST returns 405) and is
// absent from the swagger snapshot, so it was confirmed against the live surface.
//
// Create/revoke are intentionally NOT wrapped here: no /settings/{Generate,Add,
// Create,Revoke,Delete}ApiKey endpoint resolves on the external API (all 404), so
// the create/revoke verbs need the real console request to confirm before they can
// be wrapped — and a create returns a real credential, so it is gated work, not a
// guess.
package legacy

import (
	"context"
	"encoding/json"
)

// APIKey is the metadata for one SOAR external-API key. The secret value is
// deliberately omitted — the list endpoint only ever returns it masked, and this
// SDK never surfaces a key (masked or not) on a read path.
type APIKey struct {
	ID                 int      `json:"id"`
	Name               string   `json:"name"`
	IsSystem           bool     `json:"isSystem"`
	PermissionGroupID  int      `json:"permissionGroupId"`
	PermissionGroupIDs []int    `json:"permissionGroupIds"`
	SocRoleID          int      `json:"socRoleId"`
	SocRoleIDs         []int    `json:"socRoleIds"`
	Environments       []string `json:"environments"`
	CreationTimeMs     int64    `json:"creationTimeUnixTimeInMs"`
	ModificationTimeMs int64    `json:"modificationTimeUnixTimeInMs"`
}

// ListAPIKeys returns the SOAR external-API keys as metadata (no secret value).
// GET /settings/GetApiKeys. Read-only.
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	raw, err := c.externalGet(ctx, "/settings/GetApiKeys")
	if err != nil {
		return nil, err
	}
	var keys []APIKey
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []APIKey{} // stable JSON ([] not null) for scripting on an empty tenant
	}
	return keys, nil
}
