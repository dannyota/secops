package chronicle

import (
	"encoding/json"
	"testing"
)

// TestSeverityUnmarshalTolerant locks in the fix for the critical review
// finding: severity arrives as an object {"displayName":...} on custom rules
// but as a bare string ("HIGH") on curated/featured rules. Both must decode.
func TestSeverityUnmarshalTolerant(t *testing.T) {
	cases := map[string]string{
		`"HIGH"`:                 "HIGH",
		`{"displayName":"High"}`: "High",
		`null`:                   "",
		`{}`:                     "",
	}
	for in, want := range cases {
		var s Severity
		if err := json.Unmarshal([]byte(in), &s); err != nil {
			t.Fatalf("Unmarshal(%s) error: %v", in, err)
		}
		if s.DisplayName != want {
			t.Errorf("Unmarshal(%s).DisplayName = %q, want %q", in, s.DisplayName, want)
		}
	}
}

// TestSeverityAsPointerFieldDoesNotAbort verifies a bare-string severity nested
// in a struct (as on FeaturedContentRule / CuratedRuleSet) does not abort the
// whole-document decode that Client.do performs.
func TestSeverityAsPointerFieldDoesNotAbort(t *testing.T) {
	var doc struct {
		Name     string    `json:"name"`
		Severity *Severity `json:"severity"`
	}
	if err := json.Unmarshal([]byte(`{"name":"r","severity":"MEDIUM"}`), &doc); err != nil {
		t.Fatalf("decode with bare-string severity failed: %v", err)
	}
	if doc.Severity == nil || doc.Severity.DisplayName != "MEDIUM" {
		t.Errorf("severity = %+v, want DisplayName=MEDIUM", doc.Severity)
	}
	if doc.Name != "r" {
		t.Errorf("name = %q, want r (sibling fields must still decode)", doc.Name)
	}
}
