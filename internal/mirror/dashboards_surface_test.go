package mirror

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleDesiredRaw is the inline desired shape buildDesiredDashboard produces:
// charts expanded with their YARA-L query and a per-chart _server (chart+query
// resource names) that must be preserved on disk yet kept out of the diff basis.
const sampleDesiredRaw = `{
  "name": "projects/p/locations/r/instances/c/nativeDashboards/db_123",
  "displayName": "My Dashboard",
  "description": "a test dashboard",
  "type": "CUSTOM",
  "access": "DASHBOARD_PRIVATE",
  "definition": {
    "filters": [{"id": "f1", "isStandardTimeRangeFilter": true}],
    "charts": [
      {
        "title": "events over time",
        "tileType": "TILE_TYPE_VISUALIZATION",
        "chartLayout": {"startX": 0, "spanX": 12, "startY": 0, "spanY": 8},
        "filtersIds": ["f1"],
        "datasource": {"dataSources": ["UDM"]},
        "query": "metadata.event_type = \"NETWORK_DNS\"",
        "interval": {"relativeTime": {"timeUnit": "DAY", "startTimeVal": "1"}},
        "_server": {
          "chart": "projects/p/locations/r/instances/c/dashboardCharts/ch_1",
          "query": "projects/p/locations/r/instances/c/dashboardQueries/dq_1"
        }
      }
    ]
  }
}`

// TestDashboardInlineRoundTrip: an inline dashboard written to disk and reloaded
// canonicalizes identically, with the query KEPT but the per-chart _server ids
// and the root name stripped from the diff basis — and the written file still
// carries _server so push can match charts.
func TestDashboardInlineRoundTrip(t *testing.T) {
	live, err := buildDashboardObject(json.RawMessage(sampleDesiredRaw))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if live.ServerID != "projects/p/locations/r/instances/c/nativeDashboards/db_123" {
		t.Errorf("ServerID = %q", live.ServerID)
	}
	cs := string(live.Canonical)
	for _, stripped := range []string{"_server", "ch_1", "dq_1", "db_123", "\"name\""} {
		if strings.Contains(cs, stripped) {
			t.Errorf("identity %q leaked into canonical:\n%s", stripped, cs)
		}
	}
	for _, kept := range []string{"My Dashboard", "DASHBOARD_PRIVATE", "events over time", "NETWORK_DNS", "relativeTime"} {
		if !strings.Contains(cs, kept) {
			t.Errorf("config %q missing from canonical:\n%s", kept, cs)
		}
	}

	dir := t.TempDir()
	if err := writeDashboardObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The on-disk file preserves per-chart _server (needed to match on push) and
	// the top-level _server id, but never the root resource name.
	onDisk, _ := os.ReadFile(filepath.Join(dir, live.Slug+".json"))
	body := string(onDisk)
	for _, want := range []string{"_server", "ch_1", "dq_1", "NETWORK_DNS", "db_123"} {
		if !strings.Contains(body, want) {
			t.Errorf("written file missing %q:\n%s", want, body)
		}
	}
	// The root resource name lives only in the _server.id identity block (the
	// full name there is expected); the top-level "name" key must be gone.
	if strings.Contains(body, `"name"`) {
		t.Errorf("top-level \"name\" key should be dropped (identity is in _server.id):\n%s", body)
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
		t.Errorf("round-trip canonical mismatch (a fresh pull would diff dirty):\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
	// Raw is populated on load so the Update closure can match charts by _server id.
	if !strings.Contains(string(loaded[0].Raw), "ch_1") {
		t.Errorf("loaded Raw missing per-chart _server id:\n%s", loaded[0].Raw)
	}
}

// TestParseDesired extracts the inline charts (with their _server ids) used by
// Create/Update.
func TestParseDesired(t *testing.T) {
	d, err := parseDesired(json.RawMessage(sampleDesiredRaw))
	if err != nil {
		t.Fatal(err)
	}
	if d.DisplayName != "My Dashboard" || d.Access != "DASHBOARD_PRIVATE" {
		t.Errorf("dashboard = %+v", d)
	}
	if len(d.Definition.Charts) != 1 {
		t.Fatalf("charts = %d, want 1", len(d.Definition.Charts))
	}
	ch := d.Definition.Charts[0]
	if ch.Title != "events over time" || !strings.Contains(ch.Query, "NETWORK_DNS") {
		t.Errorf("chart = %+v", ch)
	}
	if ch.Server == nil || !strings.HasSuffix(ch.Server.Chart, "/ch_1") || !strings.HasSuffix(ch.Server.Query, "/dq_1") {
		t.Errorf("chart _server = %+v", ch.Server)
	}
}

// refOnlyRaw is the default-pull shape: charts carry only layout/filters + a
// _server.chart id, no inline query (no per-chart GetChart/GetQuery on pull).
const refOnlyRaw = `{
  "name": "projects/p/locations/r/instances/c/nativeDashboards/db_9",
  "displayName": "Ref Only",
  "access": "DASHBOARD_PRIVATE",
  "type": "CUSTOM",
  "definition": {
    "charts": [
      {
        "chartLayout": {"startX": 0, "spanX": 6, "startY": 0, "spanY": 4},
        "filtersIds": ["f1"],
        "_server": {"chart": "projects/p/locations/r/instances/c/dashboardCharts/ch_9"}
      }
    ]
  }
}`

// TestDashboardRefOnlyRoundTrip: a reference-only dashboard round-trips cleanly
// and never exposes the chart id or any query in the diff basis.
func TestDashboardRefOnlyRoundTrip(t *testing.T) {
	live, err := buildDashboardObject(json.RawMessage(refOnlyRaw))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cs := string(live.Canonical)
	for _, stripped := range []string{"_server", "ch_9", "db_9", "\"name\""} {
		if strings.Contains(cs, stripped) {
			t.Errorf("identity %q leaked into canonical:\n%s", stripped, cs)
		}
	}
	if strings.Contains(cs, "query") {
		t.Errorf("reference-only canonical should carry no query:\n%s", cs)
	}
	dir := t.TempDir()
	if err := writeDashboardObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadDashboards(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("ref-only round-trip mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestDashboardWantsInline: push uses the on-disk shape to decide whether to keep
// the mirror inline (query-bearing) or reference-only on refresh.
func TestDashboardWantsInline(t *testing.T) {
	if !dashboardWantsInline(json.RawMessage(sampleDesiredRaw)) {
		t.Error("inline sample should want inline")
	}
	if dashboardWantsInline(json.RawMessage(refOnlyRaw)) {
		t.Error("reference-only sample should NOT want inline")
	}
}

func TestRawEqual(t *testing.T) {
	if !rawEqual(json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"b":2,"a":1}`)) {
		t.Error("key-order-different objects should be equal")
	}
	if rawEqual(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`)) {
		t.Error("different values should not be equal")
	}
	if !rawEqual(nil, nil) {
		t.Error("two empties should be equal")
	}
	if rawEqual(json.RawMessage(`{}`), nil) {
		t.Error("empty vs non-empty should not be equal")
	}
}
