package cli

import (
	"encoding/json"
	"testing"
)

// TestExecResultRowCount pins the conservative row-count detection used by
// `dashboards verify`: it counts recognized containers and reports known=false on
// an unfamiliar shape (so a chart with an unknown result shape is never wrongly
// flagged empty).
func TestExecResultRowCount(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantCount int
		wantKnown bool
	}{
		{"rows array", `{"rows":[{"a":1},{"a":2}]}`, 2, true},
		{"empty results", `{"results":[]}`, 0, true},
		{"series", `{"series":[1,2,3]}`, 3, true},
		{"dataTable.rows", `{"dataTable":{"rows":[{"x":1}]}}`, 1, true},
		{"unknown shape", `{"something":{"nested":true}}`, 0, false},
		{"not an object", `[1,2,3]`, 0, false},
		// A populated container wins over an empty sibling (no false-empty).
		{"empty series, populated rows", `{"series":[],"rows":[{"a":1}]}`, 1, true},
		{"all recognized empty", `{"series":[],"results":[]}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, known := execResultRowCount(json.RawMessage(tc.body))
			if n != tc.wantCount || known != tc.wantKnown {
				t.Errorf("execResultRowCount(%s) = (%d,%v), want (%d,%v)", tc.body, n, known, tc.wantCount, tc.wantKnown)
			}
		})
	}
}

// TestParseFilterArg validates the --filter parsing (array, single object, empty,
// invalid).
func TestParseFilterArg(t *testing.T) {
	if f, err := parseFilterArg(""); err != nil || f != nil {
		t.Errorf("empty: got (%v,%v), want (nil,nil)", f, err)
	}
	if f, err := parseFilterArg(`[{"a":1},{"b":2}]`); err != nil || len(f) != 2 {
		t.Errorf("array: got len %d, err %v", len(f), err)
	}
	if f, err := parseFilterArg(`{"a":1}`); err != nil || len(f) != 1 {
		t.Errorf("single object: got len %d, err %v", len(f), err)
	}
	if _, err := parseFilterArg(`{not json`); err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestExecResultRowCountColumnMajor(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		count int
		known bool
	}{
		{"column-major values", `{"results":[{"column":"host","values":[{"value":{"stringVal":"a"}},{"value":{"stringVal":"b"}}]},{"column":"count","values":[{"value":{"intVal":5}}]}]}`, 2, true},
		{"columns with empty values", `{"results":[{"column":"c","values":[]}]}`, 0, true},
		{"scalar results fall through to generic arrays", `{"results":["a","b"]}`, 2, true},
	}
	for _, tc := range cases {
		count, known := execResultRowCount(json.RawMessage(tc.raw))
		if count != tc.count || known != tc.known {
			t.Errorf("%s: execResultRowCount = (%d, %v), want (%d, %v)", tc.name, count, known, tc.count, tc.known)
		}
	}
}
