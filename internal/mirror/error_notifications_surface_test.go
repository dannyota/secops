package mirror

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestErrorNotifCanonicalDropsName: the root resource name is identity (→ ServerID)
// and must not enter the diff basis; the notification block is preserved.
func TestErrorNotifCanonicalDropsName(t *testing.T) {
	raw := json.RawMessage(`{
		"name": "projects/p/locations/r/instances/c/errorNotificationConfigs/en1",
		"displayName": "zero-ingest",
		"enabled": true,
		"notificationChannels": ["projects/p/notificationChannels/123"],
		"ingestionCountZeroNotifications": {"notificationConditions": [{"logType": "WINEVTLOG", "missingDuration": "300s"}]}
	}`)
	var e chronicle.ErrorNotificationConfig
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	o, err := errorNotifObject(e)
	if err != nil {
		t.Fatal(err)
	}
	cs := string(o.Canonical)
	if strings.Contains(cs, "errorNotificationConfigs/en1") {
		t.Errorf("canonical leaked the root name:\n%s", cs)
	}
	if !strings.Contains(cs, "ingestionCountZeroNotifications") {
		t.Errorf("canonical dropped the notification block:\n%s", cs)
	}
	if o.Slug != "zero-ingest" || o.ServerID == "" {
		t.Errorf("identity wrong: slug=%q id=%q", o.Slug, o.ServerID)
	}
}

// TestErrorNotifUpdateMaskTracksPresentKeys: the mask covers exactly the writable
// keys present (the oneof variants collapse to notification_type), so a PATCH never
// clears an untouched field.
func TestErrorNotifUpdateMaskTracksPresentKeys(t *testing.T) {
	mask, err := errorNotifUpdateMask([]byte(`{"displayName":"x","enabled":true,"ingestionCountZeroNotifications":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(mask, ",")
	want := "display_name,enabled,notification_type"
	if got != want {
		t.Errorf("mask = %q, want %q", got, want)
	}
	// notification_channels was absent → must NOT be in the mask.
	for _, m := range mask {
		if m == "notification_channels" {
			t.Errorf("mask includes absent notification_channels — would clear it")
		}
	}
}
