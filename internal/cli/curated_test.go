package cli

import (
	"testing"

	"danny.vn/secops/chronicle"
)

func TestDescribeCuratedUpd(t *testing.T) {
	tr, fl := true, false
	if got := describeCuratedUpd(chronicle.CuratedDeploymentUpdate{Enabled: &tr}); got != "enabled=true" {
		t.Errorf("enabled only = %q", got)
	}
	if got := describeCuratedUpd(chronicle.CuratedDeploymentUpdate{Enabled: &fl, Alerting: &tr}); got != "enabled=false alerting=true" {
		t.Errorf("both = %q", got)
	}
}

func TestMatchCategory(t *testing.T) {
	cases := []struct {
		catID, catName, q string
		want              bool
	}{
		{"abc123", "Cloud Threats", "abc123", true},
		{"abc123", "Cloud Threats", "ABC123", true},
		{"abc123", "Cloud Threats", "cloud", true},
		{"abc123", "Cloud Threats", "Cloud Threats", true},
		{"abc123", "Cloud Threats", "xyz", false},
		{"abc123", "Cloud Threats", "threats", true},
	}
	for _, tc := range cases {
		if got := matchCategory(tc.catID, tc.catName, tc.q); got != tc.want {
			t.Errorf("matchCategory(%q, %q, %q) = %v, want %v", tc.catID, tc.catName, tc.q, got, tc.want)
		}
	}
}

func TestEnrichedRuleSetState(t *testing.T) {
	cases := []struct {
		name           string
		precise, broad bool
		wantLabel      string
		wantEnabled    bool
	}{
		{"both", true, true, "ENABLED", true},
		{"precise only", true, false, "PARTIAL", true},
		{"broad only", false, true, "PARTIAL", true},
		{"neither", false, false, "DISABLED", false},
	}
	for _, tc := range cases {
		s := enrichedRuleSet{PreciseEnabled: tc.precise, BroadEnabled: tc.broad}
		if got := s.stateLabel(); got != tc.wantLabel {
			t.Errorf("%s: stateLabel = %q, want %q", tc.name, got, tc.wantLabel)
		}
		if got := s.isEnabled(); got != tc.wantEnabled {
			t.Errorf("%s: isEnabled = %v, want %v", tc.name, got, tc.wantEnabled)
		}
	}
}

func TestFilterEnrichedSets(t *testing.T) {
	sets := []enrichedRuleSet{
		{ID: "s1", CategoryID: "c1", CategoryName: "Cloud Threats", DisplayName: "AWS - IAM", Description: "IAM access", PreciseEnabled: true},
		{ID: "s2", CategoryID: "c1", CategoryName: "Cloud Threats", DisplayName: "Azure - Network", Description: "DDoS firewall"},
		{ID: "s3", CategoryID: "c2", CategoryName: "Windows Threats", DisplayName: "Named Threat", Description: "malware", BroadEnabled: true},
	}

	cases := []struct {
		name     string
		all      bool
		category string
		search   string
		want     int
	}{
		{"enabled only", false, "", "", 2},
		{"all", true, "", "", 3},
		{"category by name", true, "cloud", "", 2},
		{"category by id", true, "c2", "", 1},
		{"search name", true, "", "azure", 1},
		{"search description", true, "", "malware", 1},
		{"enabled + search", false, "", "aws", 1},
		{"enabled + category miss", false, "windows", "", 1},
	}
	for _, tc := range cases {
		got := filterEnrichedSets(sets, tc.all, tc.category, tc.search)
		if len(got) != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, len(got), tc.want)
		}
	}
}

func TestResolvedLabel(t *testing.T) {
	m := map[string]string{"abc": "Cloud Threats"}
	if got := resolvedLabel("abc", m); got != "Cloud Threats (abc)" {
		t.Errorf("known = %q", got)
	}
	if got := resolvedLabel("xyz", m); got != "xyz" {
		t.Errorf("unknown = %q", got)
	}
	if got := resolvedLabel("abc", nil); got != "abc" {
		t.Errorf("nil map = %q", got)
	}
}
