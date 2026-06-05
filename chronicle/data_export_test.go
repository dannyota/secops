package chronicle

import (
	"encoding/json"
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
