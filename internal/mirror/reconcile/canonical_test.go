package reconcile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalizeDeterministic(t *testing.T) {
	// Same logical object, different key insertion order → identical canonical.
	a := json.RawMessage(`{"b":2,"a":1,"nested":{"y":"z","x":"w"}}`)
	b := json.RawMessage(`{"nested":{"x":"w","y":"z"},"a":1,"b":2}`)
	ca, err := Canonicalize(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Canonicalize(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca, cb) {
		t.Fatalf("canonical forms differ:\n%s\n---\n%s", ca, cb)
	}
	// Idempotent: canonicalizing the canonical bytes yields the same bytes.
	cc, err := Canonicalize(ca)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca, cc) {
		t.Fatalf("canonicalize not idempotent:\n%s\n---\n%s", ca, cc)
	}
}

func TestCanonicalizeStripsVolatile(t *testing.T) {
	raw := json.RawMessage(`{
		"id": 42,
		"etag": "abc",
		"name": "keep-me",
		"creationTimeUnixTimeInMs": 1700000000000,
		"modificationTime": "2026-01-01",
		"someThingUnixTimeInMs": 5,
		"nested": {"id": 7, "value": "kept"}
	}`)
	out, err := Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, gone := range []string{"\"id\": 42", "etag", "creationTimeUnixTimeInMs", "modificationTime", "someThingUnixTimeInMs"} {
		if strings.Contains(s, gone) {
			t.Errorf("expected %q stripped, got:\n%s", gone, s)
		}
	}
	// Top-level identity is stripped, but a NESTED id (a reference) is kept.
	if !strings.Contains(s, "\"id\": 7") {
		t.Errorf("nested id should be kept (it may be a meaningful reference):\n%s", s)
	}
	if !strings.Contains(s, "keep-me") || !strings.Contains(s, "kept") {
		t.Errorf("real config fields must survive:\n%s", s)
	}
}

func TestCanonicalizeExtraStrip(t *testing.T) {
	raw := json.RawMessage(`{"name":"x","ruleCount":9,"keep":true}`)
	out, err := Canonicalize(raw, "ruleCount")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ruleCount") {
		t.Errorf("extraStrip key not removed:\n%s", out)
	}
}

func TestDeepMergeSkipsRedactedMarker(t *testing.T) {
	const marker = "***REDACTED***"
	live := json.RawMessage(`{"name":"x","password":"realsecret","port":80}`)
	local := json.RawMessage(`{"name":"x-edited","password":"` + marker + `","port":443}`)
	merged, err := DeepMerge(live, local, func(_ string, v any) bool {
		s, ok := v.(string)
		return ok && s == marker
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "x-edited" {
		t.Errorf("edit not applied: name=%v", m["name"])
	}
	if m["password"] != "realsecret" {
		t.Errorf("redacted marker overwrote the real secret: password=%v", m["password"])
	}
	if m["port"].(float64) != 443 {
		t.Errorf("non-secret edit not applied: port=%v", m["port"])
	}
}

func TestDeepMergeArraysReplaceWholesale(t *testing.T) {
	live := json.RawMessage(`{"tags":["a","b","c"]}`)
	local := json.RawMessage(`{"tags":["x"]}`)
	merged, err := DeepMerge(live, local, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(merged, &m)
	got := m["tags"].([]any)
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("array should replace wholesale, got %v", got)
	}
}

func TestContainsValue(t *testing.T) {
	const marker = "***REDACTED***"
	if !ContainsValue(json.RawMessage(`{"a":{"b":["x","`+marker+`"]}}`), marker) {
		t.Error("should find marker nested in an array")
	}
	if ContainsValue(json.RawMessage(`{"a":"clean"}`), marker) {
		t.Error("should not find marker that isn't present")
	}
}
