package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror"
)

func TestTimeWindow(t *testing.T) {
	start, end := timeWindow(24)
	if d := end.Sub(start); d != 24*time.Hour {
		t.Errorf("window = %v, want 24h", d)
	}
	// Non-positive falls back to 24h.
	if start, end := timeWindow(0); end.Sub(start) != 24*time.Hour {
		t.Errorf("default window != 24h")
	}
	if start, end := timeWindow(168); end.Sub(start) != 168*time.Hour {
		t.Errorf("168h window wrong")
	}
}

func TestHoursOrDefault(t *testing.T) {
	if hoursOrDefault(0) != 24 || hoursOrDefault(-5) != 24 {
		t.Error("non-positive should default to 24")
	}
	if hoursOrDefault(72) != 72 {
		t.Error("positive should pass through")
	}
}

func TestOrFirst(t *testing.T) {
	if orFirst("", "b") != "b" || orFirst("a", "b") != "a" || orFirst("", "") != "" {
		t.Error("orFirst wrong")
	}
}

func TestWriteRulesAlerts(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRulesAlerts(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no alerts.") {
		t.Errorf("empty alerts: %q", buf.String())
	}

	buf.Reset()
	alerts := []json.RawMessage{json.RawMessage(`{"ruleMetadata":{"x":1}}`)}
	if err := writeRulesAlerts(&buf, alerts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ruleMetadata") {
		t.Errorf("alert body not emitted: %q", buf.String())
	}
}

func TestMatchRuleID(t *testing.T) {
	const fullA = "ru_0a1b2c3d-1111-2222-3333-444455556666"
	const fullB = "ru_0a1b2c3d-9999-2222-3333-444455556666" // shares the ru_0a1b2c3d prefix
	const fullC = "ru_aa000000-1111-2222-3333-444455556666"
	rules := []chronicle.Rule{
		{Name: "projects/p/locations/r/instances/c/rules/" + fullA, DisplayName: "My Test Rule"},
		{Name: "projects/p/locations/r/instances/c/rules/" + fullC, DisplayName: "Other Rule"},
	}
	slug := mirror.Slugify("My Test Rule")

	ok := []struct{ name, ref, want string }{
		{"full id", fullA, fullA},
		{"display name", "My Test Rule", fullA},
		{"display name case-insensitive", "my test rule", fullA},
		{"slug", slug, fullA},
		{"unique short prefix", "ru_aa000000", fullC},
		{"unlisted full id passthrough", fullB, fullB},
	}
	for _, tc := range ok {
		got, err := matchRuleID(rules, tc.ref)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		} else if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	// fullA and fullB share the ru_0a1b2c3d prefix, so a short-prefix lookup is
	// ambiguous.
	ambiguous := []chronicle.Rule{
		{Name: "x/rules/" + fullA, DisplayName: "My Test Rule"},
		{Name: "x/rules/" + fullB, DisplayName: "Dup"},
	}
	if _, err := matchRuleID(ambiguous, "ru_0a1b2c3d"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ambiguous prefix should error, got %v", err)
	}

	// Unknown reference → clean client-side error.
	if _, err := matchRuleID(rules, "no-such-rule"); err == nil ||
		!strings.Contains(err.Error(), "no rule matches") {
		t.Errorf("unknown ref should say 'no rule matches', got %v", err)
	}

	// A display name shared by two rules is ambiguous, not silently first-match.
	dupName := []chronicle.Rule{
		{Name: "x/rules/" + fullA, DisplayName: "Same Name"},
		{Name: "x/rules/" + fullC, DisplayName: "Same Name"},
	}
	if _, err := matchRuleID(dupName, "Same Name"); err == nil ||
		!strings.Contains(err.Error(), "matches 2 rules") {
		t.Errorf("duplicate display name should error, got %v", err)
	}
}

func TestLooksLikeRuleID(t *testing.T) {
	yes := []string{
		"ru_0a1b2c3d-1111-2222-3333-444455556666",
		"RU_0A1B2C3D-1111-2222-3333-444455556666", // uppercase full id still recognized
	}
	no := []string{"ru_0a1b2c3d", "My Rule", "", "ru_not-a-uuid", "0a1b2c3d-1111-2222-3333-444455556666"}
	for _, s := range yes {
		if !looksLikeRuleID(s) {
			t.Errorf("looksLikeRuleID(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeRuleID(s) {
			t.Errorf("looksLikeRuleID(%q) = true, want false", s)
		}
	}
}
