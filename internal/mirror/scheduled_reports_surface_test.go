package mirror

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestReduceDashboardRef: the embedded full dashboard collapses to just its {name}
// reference, so the report's diff tracks which dashboard it targets, not the
// dashboard's (separately managed) contents.
func TestReduceDashboardRef(t *testing.T) {
	m := map[string]any{
		"displayName": "r",
		"dashboard": map[string]any{
			"name":        "projects/p/locations/r/instances/c/nativeDashboards/d1",
			"displayName": "My Dashboard",
			"definition":  map[string]any{"charts": []any{"big", "blob"}},
		},
	}
	reduceDashboardRef(m)
	d, ok := m["dashboard"].(map[string]any)
	if !ok {
		t.Fatalf("dashboard not a map: %T", m["dashboard"])
	}
	if len(d) != 1 || d["name"] == "" {
		t.Errorf("dashboard not reduced to a bare {name}: %v", d)
	}
}

// TestScheduledReportCanonicalRoundTrips: a live report and a pulled-then-loaded
// file canonicalize equal (output-only fields stripped, dashboard reduced), so a
// fresh pull diffs as Unchanged.
func TestScheduledReportCanonicalRoundTrips(t *testing.T) {
	raw := json.RawMessage(`{
		"name": "projects/p/locations/r/instances/c/dashboardScheduledReports/sr1",
		"displayName": "Weekly",
		"description": "weekly exec report",
		"status": "ACTIVE",
		"createUserId": "someone@example.com",
		"updateUserId": "someone@example.com",
		"createTime": "2024-01-01T00:00:00Z",
		"updateTime": "2024-01-02T00:00:00Z",
		"lastSuccessfulGeneratedTime": "2024-01-02T00:00:00Z",
		"etag": "abc",
		"dashboard": {"name": "projects/p/locations/r/instances/c/nativeDashboards/d1", "displayName": "D"},
		"cronDetails": {"cron": "0 9 * * 1", "timeZone": "UTC"},
		"deliveryDetails": {"emailDelivery": {"recipients": ["a@example.com"]}, "deliveryType": "DELIVERY_TYPE_EMAIL_ATTACHMENT"},
		"format": {"fileFormat": "FILE_FORMAT_PDF"}
	}`)
	r := chronicle.DashboardScheduledReport{}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	live, err := scheduledReportObject(r)
	if err != nil {
		t.Fatal(err)
	}
	if live.Slug != "Weekly" {
		t.Errorf("slug = %q, want Weekly", live.Slug)
	}
	if live.Etag != "abc" {
		t.Errorf("etag = %q, want abc", live.Etag)
	}
	// Output-only fields must be gone from the diff basis.
	cs := string(live.Canonical)
	for _, bad := range []string{"status", "createUserId", "updateUserId", "createTime", "lastSuccessfulGeneratedTime", "\"etag\""} {
		if strings.Contains(cs, bad) {
			t.Errorf("canonical still contains output-only key %q:\n%s", bad, cs)
		}
	}
	// Write the pulled object, reload it, and confirm the canonical matches.
	dir := t.TempDir()
	if err := writeScheduledReportObject(dir, live); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadScheduledReports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d objects, want 1", len(loaded))
	}
	if string(loaded[0].Canonical) != string(live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live=%s\n disk=%s", live.Canonical, loaded[0].Canonical)
	}
	if loaded[0].ServerID != r.Name || loaded[0].Etag != "abc" {
		t.Errorf("identity not restored: id=%q etag=%q", loaded[0].ServerID, loaded[0].Etag)
	}
}

// TestReportUpdateMaskTracksPresentKeys: the updateMask must cover exactly the
// writable keys present in the body — never more — so a PATCH can't clear an
// untouched field (e.g. scope_info) to its default.
func TestReportUpdateMaskTracksPresentKeys(t *testing.T) {
	mask, err := reportUpdateMask([]byte(`{"displayName":"x","cronDetails":{"cron":"* * * * *"},"format":{"fileFormat":"FILE_FORMAT_PDF"}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(mask, ",")
	want := "cron_details,display_name,format" // sorted; absent keys excluded
	if got != want {
		t.Errorf("mask = %q, want %q (only present writable keys)", got, want)
	}
	// A key NOT in the body must NOT be in the mask (else it would be cleared).
	for _, m := range mask {
		if m == "scope_info" || m == "user_data" || m == "delivery_details" {
			t.Errorf("mask includes absent key %q — would clear it on PATCH", m)
		}
	}
}

// TestWithEtag injects the live etag into an update body (and is a no-op when empty).
func TestWithEtag(t *testing.T) {
	body, err := withEtag([]byte(`{"displayName":"x"}`), "etag-123")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m["etag"] != "etag-123" {
		t.Errorf("etag not injected: %v", m["etag"])
	}
}
