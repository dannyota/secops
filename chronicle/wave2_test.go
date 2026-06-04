package chronicle

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAvailableLogTypeSnakeCase locks the critical fix: the
// dataExports:fetchavailablelogtypes response is snake_case (unlike the
// camelCase create/get/list bodies), so the typed fields must decode from it.
func TestAvailableLogTypeSnakeCase(t *testing.T) {
	in := `{"log_type":"WINDOWS","display_name":"Windows Event Log",` +
		`"start_time":"2026-01-01T00:00:00Z","end_time":"2026-02-01T00:00:00Z"}`
	var a AvailableLogType
	if err := json.Unmarshal([]byte(in), &a); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if a.LogType != "WINDOWS" || a.DisplayName != "Windows Event Log" ||
		a.StartTime == "" || a.EndTime == "" {
		t.Errorf("snake_case decode dropped fields: %+v", a)
	}
}

// TestDetectionMatchPreservesRaw locks the fix for the forgotten unmarshaler:
// the typed fields decode AND the full object (incl. rule-varying extras) is
// preserved in Raw.
func TestDetectionMatchPreservesRaw(t *testing.T) {
	in := `{"ruleId":"ru_1","ruleName":"x","alertState":"ALERTING",` +
		`"urlBackToProduct":"https://example.com/x","ruleLabels":[{"k":"v"}]}`
	var m DetectionMatch
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.RuleID != "ru_1" || m.AlertState != "ALERTING" {
		t.Errorf("typed fields lost: %+v", m)
	}
	if len(m.Raw) == 0 || !strings.Contains(string(m.Raw), "urlBackToProduct") {
		t.Errorf("Raw must preserve rule-varying fields; got %s", m.Raw)
	}
}

// TestRefListUpdateClearEntries locks the high fix: a pointer to an empty slice
// serializes as "entries":[] (the clear-entries contract), while a
// description-only update omits the entries key entirely (no mask/body drift).
func TestRefListUpdateClearEntries(t *testing.T) {
	empty := []ReferenceListEntry{}
	b, err := json.Marshal(refListUpdateRequest{Entries: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"entries":[]`) {
		t.Errorf("clear must emit entries:[]; got %s", b)
	}

	desc := "new description"
	b2, err := json.Marshal(refListUpdateRequest{Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), "entries") {
		t.Errorf("description-only update must omit entries; got %s", b2)
	}
}
