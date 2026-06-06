package chronicle

import (
	"encoding/json"
	"testing"
)

// TestFeedServiceAccountParsing covers the response shapes of
// feedServiceAccounts:fetchServiceAccountForCustomer (the live API returns a
// numeric subjectId; other forms expose an email) and Email()'s precedence.
// All values here are synthetic.
func TestFeedServiceAccountParsing(t *testing.T) {
	// subjectId form (what the live API returns).
	var f FeedServiceAccount
	if err := json.Unmarshal([]byte(`{"subjectId":"123456789012345"}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.SubjectID != "123456789012345" {
		t.Errorf("SubjectID = %q", f.SubjectID)
	}
	if f.Email() != "" {
		t.Errorf("Email() = %q, want empty (no email field present)", f.Email())
	}
	if len(f.Raw) == 0 {
		t.Error("Raw not captured")
	}

	// serviceAccount email form.
	var g FeedServiceAccount
	_ = json.Unmarshal([]byte(`{"serviceAccount":"sts@example.iam.gserviceaccount.com"}`), &g)
	if g.Email() != "sts@example.iam.gserviceaccount.com" {
		t.Errorf("Email() = %q", g.Email())
	}

	// serviceAccountEmail fallback.
	var h FeedServiceAccount
	_ = json.Unmarshal([]byte(`{"serviceAccountEmail":"a@example.iam.gserviceaccount.com"}`), &h)
	if h.Email() != "a@example.iam.gserviceaccount.com" {
		t.Errorf("Email() = %q", h.Email())
	}
}
