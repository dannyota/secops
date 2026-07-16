package mirror

import (
	"testing"

	"danny.vn/secops/chronicle"
)

func TestSegmentAfter(t *testing.T) {
	cases := []struct{ name, marker, want string }{
		{"projects/p/locations/l/curatedRuleSets/abc/rules/r", "curatedRuleSets", "abc"},
		{"projects/p/locations/l", "curatedRuleSets", ""},
		{"a/b/marker", "marker", ""}, // marker with nothing after it
	}
	for _, tc := range cases {
		if got := segmentAfter(tc.name, tc.marker); got != tc.want {
			t.Errorf("segmentAfter(%q, %q) = %q, want %q", tc.name, tc.marker, got, tc.want)
		}
	}
}

func TestColumnHeaderFallbacks(t *testing.T) {
	cols := []chronicle.DataTableColumn{
		{OriginalColumn: "orig", Name: "named", ColumnIndex: 0},
		{Name: "named-only", ColumnIndex: 1},
		{ColumnIndex: 2},
	}
	got := columnHeader(cols)
	want := []string{"orig", "named-only", "2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("columnHeader[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if columnHeader(nil) != nil {
		t.Error("columnHeader(nil) must be nil")
	}
}

func TestIsEmptyValue(t *testing.T) {
	empties := []any{nil, "", map[string]any{}, []any{}}
	for _, v := range empties {
		if !isEmptyValue(v) {
			t.Errorf("isEmptyValue(%#v) = false, want true", v)
		}
	}
	nonEmpties := []any{"x", 0, false, map[string]any{"k": 1}, []any{1}}
	for _, v := range nonEmpties {
		if isEmptyValue(v) {
			t.Errorf("isEmptyValue(%#v) = true, want false", v)
		}
	}
}

func TestFormatStateCount(t *testing.T) {
	got := formatStateCount(map[string]int{"FAILED": 1, "ACTIVE": 2})
	want := "{'ACTIVE': 2, 'FAILED': 1}"
	if got != want {
		t.Errorf("formatStateCount = %q, want %q", got, want)
	}
	if got := formatStateCount(nil); got != "{}" {
		t.Errorf("formatStateCount(nil) = %q, want {}", got)
	}
}
