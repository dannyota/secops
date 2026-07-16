package cli

import "testing"

func TestFindRelationCycle(t *testing.T) {
	cases := []struct {
		name    string
		graph   map[string][]string
		wantCyc bool
	}{
		{"empty", nil, false},
		{"chain", map[string][]string{"a": {"b"}, "b": {"c"}}, false},
		{"diamond join is not a cycle", map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {"d"}}, false},
		{"two-node cycle", map[string][]string{"a": {"b"}, "b": {"a"}}, true},
		{"self-loop", map[string][]string{"a": {"a"}}, true},
		{"cycle behind a chain", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"b"}}, true},
	}
	for _, tc := range cases {
		cyc := findRelationCycle(tc.graph)
		if (len(cyc) > 0) != tc.wantCyc {
			t.Errorf("%s: findRelationCycle = %v, wantCycle %v", tc.name, cyc, tc.wantCyc)
		}
	}
}
