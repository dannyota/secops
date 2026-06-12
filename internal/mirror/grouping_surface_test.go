package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// A grouping rule's diff basis is the writable config only; server identity
// (name/id) and any other server-managed field are excluded, and an empty list
// canonicalizes the same on both sides.
func TestGroupingProjectionAndCanonical(t *testing.T) {
	raw := []byte(`{
	  "name": "projects/p/locations/l/instances/i/alertGroupingRules/3",
	  "id": 3,
	  "category": "ENTITIES",
	  "groupingType": "ENTITIES",
	  "entityType": ["Process", "FileName"],
	  "categoryDetails": [],
	  "serverComputed": "ignore-me"
	}`)
	proj, err := groupingProjection(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "id", "serverComputed"} {
		if _, ok := proj[k]; ok {
			t.Errorf("projection leaked server field %q", k)
		}
	}
	for _, k := range groupingConfigFields {
		if _, ok := proj[k]; !ok {
			t.Errorf("projection dropped config field %q", k)
		}
	}
	canon, err := canonicalGroupingRule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canon), "serverComputed") || strings.Contains(string(canon), "alertGroupingRules/3") {
		t.Errorf("canonical carries server-managed data: %s", canon)
	}
}

// Write a live rule, reload it, and confirm the canonical matches — a pulled rule
// diffs clean and pushes back without conversion (no phantom drift).
func TestGroupingRuleWriteLoadRoundTrips(t *testing.T) {
	raw := []byte(`{"name":"x/alertGroupingRules/3","id":3,"category":"ENTITIES","groupingType":"ENTITIES","entityType":["Process"],"categoryDetails":[]}`)
	canon, err := canonicalGroupingRule(raw)
	if err != nil {
		t.Fatal(err)
	}
	live := reconcile.Object{Slug: groupingSlug("ENTITIES", "3"), ServerID: "3", Canonical: canon, Raw: raw}

	s := GroupingRulesSurface(nil) // Write/LoadDir need no client
	dir := t.TempDir()
	if err := s.Write(dir, live); err != nil {
		t.Fatal(err)
	}
	// The on-disk file carries the server id but not the resource name.
	onDisk, err := os.ReadFile(filepath.Join(dir, live.Slug+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), `"_server"`) || strings.Contains(string(onDisk), "alertGroupingRules/3") {
		t.Errorf("on-disk shape wrong: %s", onDisk)
	}

	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != "3" {
		t.Errorf("server id = %q, want 3", loaded[0].ServerID)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("canonical differs (phantom drift):\nlive:\n%s\ndisk:\n%s", live.Canonical, loaded[0].Canonical)
	}
}

// Prune must never delete the catch-all fallback rule (category "ALL").
func TestGroupingFallbackRefusesDelete(t *testing.T) {
	s := GroupingRulesSurface(nil)
	fallback, _ := canonicalGroupingRule([]byte(`{"category":"ALL","groupingType":"ENTITIES","entityType":["Process"]}`))
	err := s.Delete(context.Background(), reconcile.Object{ServerID: "1", Canonical: fallback})
	if err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("expected a fallback-rule refusal, got %v", err)
	}
}

func TestIsGroupingFallback(t *testing.T) {
	yes, _ := json.Marshal(map[string]any{"category": "ALL"})
	no, _ := json.Marshal(map[string]any{"category": "ENTITIES"})
	if !isGroupingFallback(yes) {
		t.Error("category ALL should be the fallback")
	}
	if isGroupingFallback(no) {
		t.Error("category ENTITIES is not the fallback")
	}
}

func TestHasJSONContent(t *testing.T) {
	for _, s := range []string{"{}", "[]", "null", "", "  "} {
		if hasJSONContent([]byte(s)) {
			t.Errorf("%q should be empty content", s)
		}
	}
	if !hasJSONContent([]byte(`{"maxAlerts":50}`)) {
		t.Error("real settings should be content")
	}
}
