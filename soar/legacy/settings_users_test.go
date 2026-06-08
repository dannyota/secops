package legacy

import (
	"encoding/json"
	"testing"
)

// TestUserProfileFullName locks the NAME-column composition.
func TestUserProfileFullName(t *testing.T) {
	cases := []struct {
		first, last, want string
	}{
		{"Ada", "Lovelace", "Ada Lovelace"},
		{"Ada", "", "Ada"},
		{"", "Lovelace", "Lovelace"},
		{"", "", ""},
	}
	for _, c := range cases {
		u := UserProfile{FirstName: c.first, LastName: c.last}
		if got := u.FullName(); got != c.want {
			t.Errorf("FullName(%q,%q) = %q, want %q", c.first, c.last, got, c.want)
		}
	}
}

// TestUserProfileDecode locks the GetUserProfiles field mapping and that no secret
// / image field is surfaced on the typed view (House Rule 4 — metadata only).
func TestUserProfileDecode(t *testing.T) {
	body := `{"userName":"alice","firstName":"Alice","lastName":"Smith","email":"a@example.com",
		"socRole":"Tier1","permissionGroup":"Analysts","isDisabled":true,
		"imageBase64":"SECRET-BLOB","providerName":"saml"}`
	var u UserProfile
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.UserName != "alice" || u.Email != "a@example.com" || u.SOCRole != "Tier1" ||
		u.PermissionGroup != "Analysts" || !u.IsDisabled {
		t.Errorf("decoded profile = %+v", u)
	}
	// The typed view must not carry the avatar blob — re-marshal and confirm it's gone.
	out, _ := json.Marshal(u)
	if json.Valid(out) && containsField(out, "imageBase64") {
		t.Error("typed UserProfile leaked imageBase64")
	}
}

func containsField(b []byte, field string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	_, ok := m[field]
	return ok
}
