package mirror

import (
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestDataTapSerializationDefault: a blank format canonicalizes as MARSHALLED_PROTO
// (the server default), so a pulled tap and a format-omitting file match.
func TestDataTapSerializationDefault(t *testing.T) {
	if got := dataTapSerialization(""); got != "MARSHALLED_PROTO" {
		t.Errorf("dataTapSerialization(\"\") = %q, want MARSHALLED_PROTO", got)
	}
}

// TestDataTapObjectRoundTrips: a live tap and a format-omitting on-disk file
// canonicalize equal, and the topic/filter survive the round-trip.
func TestDataTapObjectRoundTrips(t *testing.T) {
	live, err := dataTapObject(chronicle.DataTap{
		Name:            "projects/p/locations/r/instances/c/dataTaps/tap1",
		DisplayName:     "exports",
		Filter:          chronicle.DataTapAllEvents,
		CloudPubsubSink: &chronicle.CloudPubSubSink{Topic: "projects/p/topics/t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.Slug != "exports" || live.ServerID == "" {
		t.Errorf("identity wrong: slug=%q id=%q", live.Slug, live.ServerID)
	}
	dir := t.TempDir()
	if err := writeDataTapObject(dir, live); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDataTaps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || string(loaded[0].Canonical) != string(live.Canonical) {
		t.Errorf("round-trip mismatch:\n live=%s\n disk=%v", live.Canonical, loaded)
	}
	// The default format must be present (so it never diffs against a live PROTO tap).
	if !strings.Contains(string(live.Canonical), "MARSHALLED_PROTO") {
		t.Errorf("canonical missing defaulted serialization_format:\n%s", live.Canonical)
	}
}
