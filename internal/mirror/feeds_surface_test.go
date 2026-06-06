package mirror

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

func sampleFeed() chronicle.Feed {
	return chronicle.Feed{
		Name:        "projects/p/locations/r/instances/c/feeds/fe_1",
		DisplayName: "My HTTP Feed",
		UID:         "fe_1",
		State:       "ACTIVE",
		Details: map[string]any{
			"feedSourceType": "HTTP",
			"logType":        "projects/p/locations/r/instances/c/logTypes/OKTA",
			"assetNamespace": "prod-ns",
			"labels":         map[string]any{"env": "prod"},
			"httpSettings": map[string]any{
				"uri":                 "https://example.com/ingest",
				"authorizationHeader": "super-secret-token",
			},
			// server-managed keys that must never enter the diff basis
			"lastV2MigrationAttemptTime": "2026-01-01T00:00:00Z",
			"stsMigrationReadiness":      "READY",
		},
	}
}

// TestFeedRoundTrip: a live feed written to disk and re-loaded canonicalizes
// identically — a pulled feed pushes back in sync. Also asserts the secret is
// redacted, the namespace survives, and server keys are stripped.
func TestFeedRoundTrip(t *testing.T) {
	live, err := feedLiveObject(sampleFeed())
	if err != nil {
		t.Fatalf("live object: %v", err)
	}
	// Secret never in the canonical; namespace present; server keys stripped.
	cs := string(live.Canonical)
	if strings.Contains(cs, "super-secret-token") {
		t.Error("secret leaked into canonical")
	}
	if !strings.Contains(cs, redactedMarker) {
		t.Error("expected a redaction marker in canonical")
	}
	if !strings.Contains(cs, "prod-ns") {
		t.Error("assetNamespace lost from canonical (the read/write key bug)")
	}
	for _, k := range []string{"lastV2MigrationAttemptTime", "stsMigrationReadiness"} {
		if strings.Contains(cs, k) {
			t.Errorf("server-managed key %q leaked into canonical", k)
		}
	}

	dir := t.TempDir()
	if err := writeFeedObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadFeeds(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != "projects/p/locations/r/instances/c/feeds/fe_1" {
		t.Errorf("ServerID = %q", loaded[0].ServerID)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestFeedUpdateOverlayPreservesSecret models the Update overlay: the local
// (redacted) canonical merged onto the live unredacted spec must keep the real
// secret (mask never wins) while applying a genuine edit.
func TestFeedUpdateOverlayPreservesSecret(t *testing.T) {
	live := sampleFeed()
	localCanon, err := feedLiveObject(live) // same as a freshly pulled file
	if err != nil {
		t.Fatal(err)
	}
	// Operator edits a non-secret field (the URI) on disk; secret stays masked.
	edited := strings.Replace(string(localCanon.Canonical), "https://example.com/ingest", "https://example.com/v2", 1)

	liveSpecJSON, _ := json.Marshal(feedSpecFromFeed(live))
	merged, err := reconcile.DeepMerge(liveSpecJSON, json.RawMessage(edited), func(_ string, v any) bool {
		s, ok := v.(string)
		return ok && s == redactedMarker
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	spec, err := decodeFeedSpec(merged)
	if err != nil {
		t.Fatal(err)
	}
	http, _ := spec.Settings["httpSettings"].(map[string]any)
	if got := http["authorizationHeader"]; got != "super-secret-token" {
		t.Errorf("overlay did not preserve the real secret: got %v", got)
	}
	if got := http["uri"]; got != "https://example.com/v2" {
		t.Errorf("overlay did not apply the edit: got %v", got)
	}
}

// TestFeedWriteSettingsFoldsLabels: labels must be folded back into the settings
// the SDK merges into details (the API carries labels inside details).
func TestFeedWriteSettingsFoldsLabels(t *testing.T) {
	spec := feedSpec{
		Labels:   map[string]any{"env": "prod"},
		Settings: map[string]any{"httpSettings": map[string]any{"uri": "x"}},
	}
	got := feedWriteSettings(spec)
	if _, ok := got["labels"]; !ok {
		t.Error("labels not folded into write settings")
	}
	if _, ok := got["httpSettings"]; !ok {
		t.Error("settings dropped from write settings")
	}
}
