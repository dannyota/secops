package mirror

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

func sampleForwarder() chronicle.Forwarder {
	return chronicle.Forwarder{
		Name:        "projects/p/locations/r/instances/c/forwarders/fwd_1",
		DisplayName: "My Forwarder",
		Config: json.RawMessage(`{
			"uploadCompression": true,
			"metadata": {"assetNamespace": "prod-ns"},
			"serverSettings": {"enabled": false},
			"state": "ACTIVE",
			"createTime": "2026-01-01T00:00:00Z",
			"updateTime": "2026-01-02T00:00:00Z"
		}`),
	}
}

// TestForwarderRoundTrip: a live forwarder written to disk and re-loaded
// canonicalizes identically — a pulled forwarder pushes back in sync. Also
// asserts runtime/server keys (state) and server-stamped times are stripped from
// the diff basis while real config survives.
func TestForwarderRoundTrip(t *testing.T) {
	live, err := forwarderLiveObject(sampleForwarder())
	if err != nil {
		t.Fatalf("live object: %v", err)
	}
	cs := string(live.Canonical)
	if strings.Contains(cs, `"state"`) {
		t.Error("runtime key 'state' leaked into canonical")
	}
	for _, k := range []string{"createTime", "updateTime"} {
		if strings.Contains(cs, k) {
			t.Errorf("server-stamped time key %q leaked into canonical", k)
		}
	}
	if !strings.Contains(cs, "prod-ns") {
		t.Error("config metadata lost from canonical")
	}
	if !strings.Contains(cs, "serverSettings") {
		t.Error("config serverSettings lost from canonical")
	}

	dir := t.TempDir()
	if err := writeForwarderObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadForwarders(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != "projects/p/locations/r/instances/c/forwarders/fwd_1" {
		t.Errorf("ServerID = %q", loaded[0].ServerID)
	}
	if loaded[0].Slug != live.Slug {
		t.Errorf("slug: loaded %q != live %q", loaded[0].Slug, live.Slug)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestForwarderUpdateOverlay models the Update overlay: a local edit merged onto
// the live config replaces the edited field while leaving untouched config intact.
func TestForwarderUpdateOverlay(t *testing.T) {
	live := sampleForwarder()
	localCanon, err := forwarderLiveObject(live) // same as a freshly pulled file
	if err != nil {
		t.Fatal(err)
	}
	// Operator flips uploadCompression on disk.
	edited := strings.Replace(string(localCanon.Canonical),
		`"uploadCompression": true`, `"uploadCompression": false`, 1)

	liveCfg, _ := forwarderConfigMap(live)
	liveSpecJSON, _ := json.Marshal(fwdSpec{DisplayName: forwarderDisplay(live), Config: liveCfg})
	merged, err := reconcile.DeepMerge(liveSpecJSON, json.RawMessage(edited), nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	spec, err := decodeForwarderSpec(merged)
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Config["uploadCompression"]; got != false {
		t.Errorf("overlay did not apply the edit: uploadCompression = %v", got)
	}
	ss, _ := spec.Config["serverSettings"].(map[string]any)
	if ss == nil || ss["enabled"] != false {
		t.Errorf("overlay dropped untouched config: serverSettings = %v", spec.Config["serverSettings"])
	}
}

// TestForwarderBody confirms the create/update body carries displayName + config.
func TestForwarderBody(t *testing.T) {
	body := forwarderBody(fwdSpec{
		DisplayName: "f1",
		Config:      map[string]any{"uploadCompression": true},
	})
	if body["displayName"] != "f1" {
		t.Errorf("displayName = %v", body["displayName"])
	}
	if _, ok := body["config"]; !ok {
		t.Error("config dropped from body")
	}

	// A config-less spec must not emit a config key.
	if _, ok := forwarderBody(fwdSpec{DisplayName: "f2"})["config"]; ok {
		t.Error("empty config should not appear in the body")
	}
}
