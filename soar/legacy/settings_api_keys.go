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
// Create and revoke ride the same surface: POST /settings/addOrUpdateApiKeyRecord
// (no id = create; an id present = update; the KEY VALUE IS CLIENT-GENERATED and
// the server only ever returns it masked) and POST /settings/RemoveApiKeyRecord
// (the full record as listed). Both are absent from the swagger snapshot.
package legacy

import (
	"context"
	"encoding/json"
	"fmt"
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

	// Raw is the verbatim list entry (key field masked by the server) — the
	// exact body RemoveApiKeyRecord expects. Never carries a usable secret.
	Raw json.RawMessage `json:"-"`
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

// UnmarshalJSON decodes the typed metadata and retains the verbatim list
// entry in Raw — RemoveApiKeyRecord expects the FULL record as listed (with
// the key field masked by the server), so the raw form is what revoke posts.
func (k *APIKey) UnmarshalJSON(data []byte) error {
	type alias APIKey
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*k = APIKey(a)
	k.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// apiKeyCreateBody is the addOrUpdateApiKeyRecord create payload: no id (an
// id present means update), the client-minted key value, and the permission
// scope. SocRoleID stays null on create — the server defaults the role.
type apiKeyCreateBody struct {
	Name               string   `json:"name"`
	Key                string   `json:"key"`
	Environments       []string `json:"environments"`
	PermissionGroupIDs []int    `json:"permissionGroupIds"`
	PermissionGroupID  int      `json:"permissionGroupId"`
	SocRoleID          *int     `json:"socRoleId"`
	SocRoleIDs         []int    `json:"socRoleIds"`
}

// CreateAPIKey registers a new external-API key record
// (POST /settings/addOrUpdateApiKeyRecord). THE KEY VALUE IS CLIENT-GENERATED:
// the server stores the supplied secret verbatim and only ever returns it
// masked afterwards — mint it from crypto/rand, surface it to the user exactly
// once, and never persist or log it. socRoleID <= 0 sends null (the server
// defaults the role). LIVE MUTATION.
func (c *Client) CreateAPIKey(ctx context.Context, name, secret string, permissionGroupID, socRoleID int, environments []string) error {
	if name == "" || secret == "" {
		return fmt.Errorf("legacy: api-key name and secret are required")
	}
	if permissionGroupID <= 0 {
		return fmt.Errorf("legacy: a permission group id is required")
	}
	if len(environments) == 0 {
		environments = []string{"*"}
	}
	body := apiKeyCreateBody{
		Name:               name,
		Key:                secret,
		Environments:       environments,
		PermissionGroupIDs: []int{permissionGroupID},
		PermissionGroupID:  permissionGroupID,
		SocRoleIDs:         []int{},
	}
	if socRoleID > 0 {
		body.SocRoleID = &socRoleID
		body.SocRoleIDs = []int{socRoleID}
	}
	_, err := c.externalPost(ctx, "/settings/addOrUpdateApiKeyRecord", body)
	return err
}

// RevokeAPIKey removes one key record (POST /settings/RemoveApiKeyRecord).
// The endpoint expects the full record as listed, so rec must come from
// ListAPIKeys (its Raw form is posted verbatim). LIVE MUTATION.
func (c *Client) RevokeAPIKey(ctx context.Context, rec APIKey) error {
	if len(rec.Raw) == 0 {
		return fmt.Errorf("legacy: revoke needs a record from ListAPIKeys (raw form missing)")
	}
	_, err := c.externalPost(ctx, "/settings/RemoveApiKeyRecord", rec.Raw)
	return err
}
