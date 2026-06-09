package mirror

import (
	"bytes"
	"testing"

	"danny.vn/secops/chronicle"
)

func sampleRuleExclusion() chronicle.RuleExclusion {
	return chronicle.RuleExclusion{
		Name:        "projects/p/locations/r/instances/c/findingsRefinements/fr_123",
		DisplayName: "Suppress scanner host",
		Type:        chronicle.DetectionExclusion,
		Query:       `principal.hostname = "example-scanner-host"`,
		Etag:        "etag-xyz",          // volatile — must not affect canonical
		CreateTime:  "2026-01-01T00:00Z", // volatile
	}
}

func sampleRuleExclusionDeployment() *chronicle.RuleExclusionDeployment {
	return &chronicle.RuleExclusionDeployment{
		Name:        "projects/p/locations/r/instances/c/findingsRefinements/fr_123/deployment",
		Enabled:     true,
		Archived:    false,
		ArchiveTime: "2026-01-02T00:00Z", // volatile
		Etag:        "deployment-etag",
	}
}

// TestRuleExclusionRoundTrip: a live exclusion written to disk and re-loaded
// canonicalizes identically (a pulled exclusion pushes back in sync).
func TestRuleExclusionRoundTrip(t *testing.T) {
	live, err := ruleExclusionObject(sampleRuleExclusion(), sampleRuleExclusionDeployment())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cs := string(live.Canonical)
	if bytes.Contains(live.Canonical, []byte("etag-xyz")) ||
		bytes.Contains(live.Canonical, []byte("deployment-etag")) ||
		bytes.Contains(live.Canonical, []byte("fr_123")) {
		t.Errorf("volatile/identity leaked into canonical:\n%s", cs)
	}
	if !bytes.Contains(live.Canonical, []byte(`"deployment"`)) {
		t.Errorf("deployment state missing from canonical:\n%s", cs)
	}

	dir := t.TempDir()
	if err := writeRuleExclusion(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadRuleExclusions(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != "projects/p/locations/r/instances/c/findingsRefinements/fr_123" {
		t.Errorf("ServerID = %q", loaded[0].ServerID)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestRuleExclusionCanonicalFields: query + type + display + deployment are the diff basis.
func TestRuleExclusionCanonicalFields(t *testing.T) {
	live, _ := ruleExclusionObject(sampleRuleExclusion(), sampleRuleExclusionDeployment())
	spec, err := decodeRuleExclusionSpec(live.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	if spec.DisplayName != "Suppress scanner host" || spec.Type != "DETECTION_EXCLUSION" ||
		spec.Query != `principal.hostname = "example-scanner-host"` ||
		spec.Deployment == nil ||
		!spec.Deployment.Enabled ||
		spec.Deployment.Archived {
		t.Errorf("spec = %+v", spec)
	}
}
