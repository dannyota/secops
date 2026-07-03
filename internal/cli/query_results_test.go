package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractUDMField(t *testing.T) {
	cases := []struct {
		name, ev, path, want string
	}{
		{"udm root snake", `{"udm":{"metadata":{"event_type":"NETWORK_DNS"}}}`, "metadata.event_type", "NETWORK_DNS"},
		{"udm root camel via snake path", `{"udm":{"metadata":{"eventType":"NETWORK_DNS"}}}`, "metadata.event_type", "NETWORK_DNS"},
		{"event root (view shape)", `{"event":{"principal":{"hostname":"h1"}},"uid":"x"}`, "principal.hostname", "h1"},
		{"bare udm object", `{"metadata":{"eventType":"X"}}`, "metadata.event_type", "X"},
		{"scalar array joined", `{"udm":{"principal":{"ip":["1.1.1.1","2.2.2.2"]}}}`, "principal.ip", "1.1.1.1,2.2.2.2"},
		{"missing field", `{"udm":{"metadata":{}}}`, "principal.hostname", ""},
		{"number leaf", `{"udm":{"network":{"sent_bytes":42}}}`, "network.sent_bytes", "42"},
		{"singleton array auto-enter", `{"udm":{"principal":{"ipGeoArtifact":[{"network":{"asn":"7552"}}]}}}`, "principal.ipGeoArtifact.network.asn", "7552"},
		{"multi-element array no auto-enter", `{"udm":{"principal":{"ipGeoArtifact":[{"network":{"asn":"1"}},{"network":{"asn":"2"}}]}}}`, "principal.ipGeoArtifact.network.asn", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractUDMField(json.RawMessage(c.ev), c.path); got != c.want {
				t.Errorf("extractUDMField(%s) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestRenderCSVProjection(t *testing.T) {
	events := []json.RawMessage{
		json.RawMessage(`{"udm":{"metadata":{"event_type":"NETWORK_DNS"},"principal":{"hostname":"h1"}}}`),
		json.RawMessage(`{"udm":{"metadata":{"event_type":"NETWORK_CONNECTION"},"principal":{"hostname":"h2"}}}`),
	}
	var buf bytes.Buffer
	if err := renderCSV(&buf, events, []string{"metadata.event_type", "principal.hostname"}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"metadata.event_type,principal.hostname", "NETWORK_DNS,h1", "NETWORK_CONNECTION,h2"} {
		if !strings.Contains(got, want) {
			t.Errorf("CSV missing %q; got:\n%s", want, got)
		}
	}
}

func TestRenderJSONLProjection(t *testing.T) {
	events := []json.RawMessage{json.RawMessage(`{"udm":{"metadata":{"event_type":"NETWORK_DNS"}}}`)}
	var buf bytes.Buffer
	if err := renderJSONL(&buf, events, []string{"metadata.event_type"}); err != nil {
		t.Fatal(err)
	}
	var row map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &row); err != nil {
		t.Fatalf("jsonl line not an object: %v (%s)", err, buf.String())
	}
	if row["metadata.event_type"] != "NETWORK_DNS" {
		t.Errorf("projected row = %v", row)
	}
}

func TestRenderJSONLFullEvent(t *testing.T) {
	events := []json.RawMessage{json.RawMessage(`{"udm":{"metadata":{"event_type":"X"}}}`), json.RawMessage(`{"udm":{"metadata":{"event_type":"Y"}}}`)}
	var buf bytes.Buffer
	if err := renderJSONL(&buf, events, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines, got %d: %s", len(lines), buf.String())
	}
}

func TestResolveFormatJSONFlag(t *testing.T) {
	defer func(prev bool) { jsonOut = prev }(jsonOut)
	jsonOut = true
	if got := (resultOutput{}).resolveFormat(); got != formatJSON {
		t.Errorf("with --json, resolveFormat() = %q, want json", got)
	}
	jsonOut = false
	if got := (resultOutput{format: formatCSV}).resolveFormat(); got != formatCSV {
		t.Errorf("explicit --format wins: got %q", got)
	}
}

func TestSplitFields(t *testing.T) {
	got := splitFields(" a , b ,, c ")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("splitFields = %v", got)
	}
	if splitFields("  ") != nil {
		t.Error("empty splitFields should be nil")
	}
}
