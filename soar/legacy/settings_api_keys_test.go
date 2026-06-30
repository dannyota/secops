package legacy

import (
	"context"
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

// TestCreateAPIKeyBodyShape pins the addOrUpdateApiKeyRecord create payload:
// no id field (id present = update), the client-minted key value verbatim,
// permission scope as both scalar and list, socRoleId null unless given.
func TestCreateAPIKeyBodyShape(t *testing.T) {
	rt := &captureRT{resp: `{}`}
	c := newCaptureClient(rt)
	if err := c.CreateAPIKey(context.Background(), "robot", "0f0e0d0c-0b0a-0908-0706-050403020100", 2, 0, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rt.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, hasID := body["id"]; hasID {
		t.Error("create body must not carry an id (id = update)")
	}
	if string(body["key"]) != `"0f0e0d0c-0b0a-0908-0706-050403020100"` {
		t.Errorf("key = %s", body["key"])
	}
	if string(body["socRoleId"]) != "null" {
		t.Errorf("socRoleId = %s, want null (server default)", body["socRoleId"])
	}
	if string(body["permissionGroupIds"]) != "[2]" || string(body["permissionGroupId"]) != "2" {
		t.Errorf("permission scope = %s / %s", body["permissionGroupIds"], body["permissionGroupId"])
	}
	if string(body["environments"]) != `["*"]` {
		t.Errorf("environments default = %s", body["environments"])
	}

	if err := c.CreateAPIKey(context.Background(), "", "s", 1, 0, nil); err == nil {
		t.Error("empty name must error")
	}
	if err := c.CreateAPIKey(context.Background(), "n", "s", 0, 0, nil); err == nil {
		t.Error("missing permission group must error")
	}
}

// TestRevokeAPIKeyPostsRawRecord locks that revoke posts the record exactly
// as listed (the endpoint wants the full row), and refuses without one.
func TestRevokeAPIKeyPostsRawRecord(t *testing.T) {
	listed := `{"id":7,"name":"robot","key":"****","isSystem":false,"permissionGroupId":2,"permissionGroupIds":[2],"environments":["*"],"socRoleId":1,"socRoleIds":[1],"creationTimeUnixTimeInMs":1,"modificationTimeUnixTimeInMs":1}`
	var rec APIKey
	if err := json.Unmarshal([]byte(listed), &rec); err != nil {
		t.Fatal(err)
	}
	rt := &captureRT{resp: `{}`}
	c := newCaptureClient(rt)
	if err := c.RevokeAPIKey(context.Background(), rec); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if rt.body != listed {
		t.Errorf("revoke body = %s, want the verbatim listed record", rt.body)
	}
	if err := c.RevokeAPIKey(context.Background(), APIKey{ID: 7}); err == nil {
		t.Error("revoke without the raw record must error")
	}
}

// TestNewAiGenerateRequest pins the legacyAiGenerate envelope: prompt + an
// EMPTY unsaved draft (id "0", empty steps) and NO creationSource (the
// endpoint rejects it).
func TestNewAiGenerateRequest(t *testing.T) {
	req, err := NewAiGenerateRequest("close informative cases", "draft-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "creationSource") {
		t.Error("envelope must not carry creationSource")
	}
	var m struct {
		Prompt   string `json:"prompt"`
		Playbook struct {
			ID           string `json:"id"`
			Identifier   string `json:"identifier"`
			Steps        []any  `json:"steps"`
			PlaybookType string `json:"playbookType"`
		} `json:"playbook"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Prompt == "" || m.Playbook.ID != "0" || m.Playbook.Steps == nil || len(m.Playbook.Steps) != 0 || m.Playbook.PlaybookType != "REGULAR" {
		t.Errorf("envelope = %s", s)
	}
	if len(m.Playbook.Identifier) != 36 {
		t.Errorf("identifier %q is not a UUID", m.Playbook.Identifier)
	}
	if _, err := NewAiGenerateRequest("", "x"); err == nil {
		t.Error("empty prompt must error")
	}
}
