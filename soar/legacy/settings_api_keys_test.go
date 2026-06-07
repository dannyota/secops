package legacy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAPIKeyDecodeDropsSecret: the typed APIKey decodes the metadata and, even when
// the server payload includes a (masked) `key` field, never captures or surfaces
// it — the struct has no key field at all.
func TestAPIKeyDecodeDropsSecret(t *testing.T) {
	// Placeholder payload shaped like GetApiKeys (key masked, as the server returns).
	raw := `[{
		"id": 7,
		"name": "ci-bot@example.com",
		"key": "****************************",
		"isSystem": false,
		"permissionGroupId": 1,
		"permissionGroupIds": [1],
		"socRoleId": 2,
		"socRoleIds": [2],
		"environments": ["*"],
		"creationTimeUnixTimeInMs": 1700000000000,
		"modificationTimeUnixTimeInMs": 1700000000000
	}]`
	var keys []APIKey
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
	k := keys[0]
	if k.ID != 7 || k.Name != "ci-bot@example.com" || k.PermissionGroupID != 1 || k.SocRoleID != 2 {
		t.Errorf("metadata decoded wrong: %+v", k)
	}
	// Re-marshal the typed view and confirm no key/secret leaks through it.
	out, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(out)), "\"key\"") || strings.Contains(string(out), "****") {
		t.Errorf("typed APIKey surfaced a key value: %s", out)
	}
}
