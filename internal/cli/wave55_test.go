package cli

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodeExportBundle(t *testing.T) {
	// ApiFile envelope: blob decodes from base64.
	payload := []byte("PK\x03\x04fake-zip-bytes")
	wrapped, _ := json.Marshal(map[string]string{
		"fileName": "export.zip",
		"blob":     base64.StdEncoding.EncodeToString(payload),
	})
	got, err := decodeExportBundle(wrapped)
	if err != nil || string(got) != string(payload) {
		t.Errorf("ApiFile decode = %q, %v", got, err)
	}
	// Anything else passes through untouched.
	raw := json.RawMessage(`{"some":"object"}`)
	got, err = decodeExportBundle(raw)
	if err != nil || string(got) != string(raw) {
		t.Errorf("raw passthrough = %q, %v", got, err)
	}
	// A corrupt blob errors rather than writing garbage.
	bad := json.RawMessage(`{"fileName":"x.zip","blob":"%%%not-base64%%%"}`)
	if _, err = decodeExportBundle(bad); err == nil {
		t.Error("corrupt blob must error")
	}
	// An ApiFile envelope with an EMPTY blob is a failed export, not a bundle —
	// silently writing the envelope would fake a successful backup.
	empty := json.RawMessage(`{"fileName":"x.zip","blob":""}`)
	if _, err = decodeExportBundle(empty); err == nil {
		t.Error("empty blob must error")
	}
}

func TestEmitPlaybookVersionsEmptyWrap(t *testing.T) {
	// A present-but-empty wrap key is a valid empty result, not an unknown shape.
	for _, raw := range []string{`{"items":[]}`, `{"objectsList":[]}`, `[]`} {
		if err := emitPlaybookVersions(json.RawMessage(raw)); err != nil {
			t.Errorf("empty result %s: %v", raw, err)
		}
	}
}

func TestTriggerTagRecords(t *testing.T) {
	recs, ok := triggerTagRecords(json.RawMessage(`["a","b"]`))
	if !ok || len(recs) != 2 {
		t.Errorf("bare array: %v %v", recs, ok)
	}
	recs, ok = triggerTagRecords(json.RawMessage(`{"objectsList":[{"name":"x"}]}`))
	if !ok || len(recs) != 1 {
		t.Errorf("objectsList wrap: %v %v", recs, ok)
	}
	if _, ok = triggerTagRecords(json.RawMessage(`"scalar"`)); ok {
		t.Error("unrecognized shape must report !ok")
	}
}

func TestEmitPlaybookVersionsDecode(t *testing.T) {
	raw := json.RawMessage(`[
	  {"identifier":"v-2","version":2,"comment":"fix trigger","creator":"analyst","creationTimeUnixTimeInMs":1764768000000},
	  {"identifier":"v-1","version":1,"comment":"","creator":"analyst","creationTimeUnixTimeInMs":1764681600000}
	]`)
	var entries []playbookVersionView
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Identifier != "v-2" || entries[0].Version != 2 {
		t.Errorf("decode = %+v", entries)
	}
}

func TestEmitWorkflowsInvolvingActionShapes(t *testing.T) {
	// A bare string array renders; a non-array shape falls back to raw JSON.
	if err := emitWorkflowsInvolvingAction(json.RawMessage(`["PB One","PB Two"]`)); err != nil {
		t.Errorf("names array: %v", err)
	}
	if err := emitWorkflowsInvolvingAction(json.RawMessage(`{"items":[]}`)); err != nil {
		t.Errorf("raw fallback: %v", err)
	}
}
