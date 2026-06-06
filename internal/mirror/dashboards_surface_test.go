package mirror

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// sampleDashboardRaw is a full-view CUSTOM dashboard body as the API returns it,
// including the server/volatile keys that must be stripped from the diff basis.
const sampleDashboardRaw = `{
  "name": "projects/p/locations/r/instances/c/nativeDashboards/db_123",
  "displayName": "My Dashboard",
  "description": "a test dashboard",
  "type": "CUSTOM",
  "access": "DASHBOARD_PRIVATE",
  "etag": "etag-abc",
  "createTime": "2026-01-01T00:00:00Z",
  "updateTime": "2026-02-02T00:00:00Z",
  "createUserId": "user-1",
  "updateUserId": "user-2",
  "dashboardUserData": {"lastViewedTime": "2026-03-03T00:00:00Z"},
  "definition": {
    "filters": [{"id": "f1", "isStandardTimeRangeFilter": true}],
    "charts": [{"filtersIds": ["f1"], "title": "events over time"}]
  }
}`

// TestDashboardRoundTrip: a live CUSTOM dashboard written to disk and re-loaded
// canonicalizes identically, with server/volatile keys stripped and config kept.
func TestDashboardRoundTrip(t *testing.T) {
	live, err := buildDashboardObject(json.RawMessage(sampleDashboardRaw))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if live.ServerID != "projects/p/locations/r/instances/c/nativeDashboards/db_123" {
		t.Errorf("ServerID = %q", live.ServerID)
	}
	cs := string(live.Canonical)
	for _, stripped := range []string{"createUserId", "updateUserId", "dashboardUserData", "lastViewedTime", "etag-abc", "db_123"} {
		if strings.Contains(cs, stripped) {
			t.Errorf("volatile/identity %q leaked into canonical:\n%s", stripped, cs)
		}
	}
	for _, kept := range []string{"My Dashboard", "DASHBOARD_PRIVATE", "events over time", "\"type\""} {
		if !strings.Contains(cs, kept) {
			t.Errorf("config %q missing from canonical:\n%s", kept, cs)
		}
	}

	dir := t.TempDir()
	if err := writeDashboardObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadDashboards(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != live.ServerID {
		t.Errorf("loaded ServerID = %q, want %q", loaded[0].ServerID, live.ServerID)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestParseDashboardConfig extracts the writable fields used by Create/Update.
func TestParseDashboardConfig(t *testing.T) {
	live, _ := buildDashboardObject(json.RawMessage(sampleDashboardRaw))
	cfg, err := parseDashboardConfig(live.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisplayName != "My Dashboard" || cfg.Access != "DASHBOARD_PRIVATE" {
		t.Errorf("config = %+v", cfg)
	}
	if len(cfg.Definition.Charts) != 1 || len(cfg.Definition.Filters) != 1 {
		t.Errorf("definition not parsed: %d filters, %d charts", len(cfg.Definition.Filters), len(cfg.Definition.Charts))
	}
}
