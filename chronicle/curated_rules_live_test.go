package chronicle_test

import (
	"testing"
)

// TestLiveCuratedRulesRead validates the curated-rules read path: list the
// Google-managed curated rules, then GET one back by short id (round-trip).
// Read-only. Gated on SECOPS_SIEM_SMOKE=1.
func TestLiveCuratedRulesRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	rules, err := c.ListCuratedRules(ctx)
	if err != nil {
		t.Fatalf("ListCuratedRules: %v", err)
	}
	t.Logf("listed %d curated rule(s)", len(rules))
	if len(rules) == 0 {
		t.Skip("tenant has no curated rules")
	}
	if rules[0].ID == "" {
		t.Fatalf("curated rule has empty derived ID (name=%q)", rules[0].Name)
	}

	got, err := c.GetCuratedRule(ctx, rules[0].ID)
	if err != nil {
		t.Fatalf("GetCuratedRule(%q): %v", rules[0].ID, err)
	}
	if got.ID != rules[0].ID {
		t.Fatalf("round-trip id mismatch: list %q vs get %q", rules[0].ID, got.ID)
	}
}
