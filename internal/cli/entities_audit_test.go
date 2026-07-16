package cli

import (
	"encoding/json"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestEntityIndicatorStrings(t *testing.T) {
	raw := json.RawMessage(`{
		"metadata": {"entityType": "USER", "vendor": "ignored-not-under-entity"},
		"entity": {
			"user": {"userid": "alice", "email_addresses": ["alice@example.com"]},
			"asset": {"ip": ["203.0.113.7"]}
		}
	}`)
	got := entityIndicatorStrings(raw)
	want := map[string]bool{"alice": true, "alice@example.com": true, "203.0.113.7": true}
	if len(got) != len(want) {
		t.Fatalf("entityIndicatorStrings = %v, want keys %v", got, want)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected indicator %q", s)
		}
	}
	if got := entityIndicatorStrings(json.RawMessage(`{"metadata":{}}`)); got != nil {
		t.Errorf("no entity subtree should yield nil, got %v", got)
	}
}

func scoreFor(indicator string, risk int) chronicle.EntityRiskScore {
	return chronicle.EntityRiskScore{EntityIndicator: map[string]string{"indicator": indicator}, RiskScore: risk}
}

func TestBuildAuditResultCrossReference(t *testing.T) {
	scores := []chronicle.EntityRiskScore{
		scoreFor("Alice", 900), // on a watchlist (case-insensitive match)
		scoreFor("bob", 800),   // not on any watchlist → gap
		scoreFor("carol", 100), // below threshold
	}
	members := map[string]bool{"alice": true}

	r := buildAuditResult(nil, scores, 500, members)
	if r.HighRiskCount != 2 {
		t.Errorf("HighRiskCount = %d, want 2", r.HighRiskCount)
	}
	if r.GapCount != 1 || len(r.CoverageGaps) != 1 || r.CoverageGaps[0].Entity != "bob" {
		t.Errorf("CoverageGaps = %+v, want only bob", r.CoverageGaps)
	}

	// nil membership = cross-reference unavailable: every high-risk entity listed.
	r = buildAuditResult(nil, scores, 500, nil)
	if r.GapCount != 2 {
		t.Errorf("unchecked GapCount = %d, want 2", r.GapCount)
	}
}
