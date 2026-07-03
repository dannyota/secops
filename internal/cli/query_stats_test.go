package cli

import (
	"encoding/json"
	"testing"
)

// TestQueryFlagsInterspersed pins that a flag placed AFTER the positional query
// is still parsed — `search udm '<q>' --hours 6` must not fail as an unknown
// flag. cobra/pflag default to interspersed parsing; this guards against a
// future regression that disables it.
func TestQueryFlagsInterspersed(t *testing.T) {
	for _, path := range [][]string{{"search", "udm"}, {"search", "stats"}, {"search", "raw"}} {
		cmd, _, err := rootCmd.Find(path)
		if err != nil || cmd == nil {
			t.Fatalf("find %v: %v", path, err)
		}
		fs := cmd.Flags()
		if err := fs.Parse([]string{"the-positional-query", "--hours", "6"}); err != nil {
			t.Errorf("%v: flag after positional failed to parse: %v", path, err)
			continue
		}
		// With interspersed parsing, the only leftover positional is the query and
		// --hours was consumed as a flag.
		if args := fs.Args(); len(args) != 1 || args[0] != "the-positional-query" {
			t.Errorf("%v: flag after positional not parsed (interspersed off?): leftover args = %v", path, args)
		}
	}
}

// TestStatsCell renders stats cells (raw JSON) compactly.
func TestStatsCell(t *testing.T) {
	cases := map[string]string{
		`"prod"`: "prod",
		`42`:     "42",
		``:       "-",
	}
	for in, want := range cases {
		if got := statsCell(json.RawMessage(in)); got != want {
			t.Errorf("statsCell(%q) = %q, want %q", in, got, want)
		}
	}

	// Array rendering: array()/array_distinct() outcomes.
	arrayCases := []struct {
		in   string
		want string
	}{
		{`["a","b","c"]`, "a, b, c"},
		{`[1,2,3]`, "1, 2, 3"},
		{`["single"]`, "single"},
		{`[]`, "[]"},
	}
	for _, tc := range arrayCases {
		if got := statsCell(json.RawMessage(tc.in)); got != tc.want {
			t.Errorf("statsCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsAggregationQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"plain filter", `metadata.event_type = "USER_LOGIN"`, false},
		{"match and outcome", "metadata.log_type != \"\"\nmatch: metadata.log_type\noutcome: $c = count(metadata.id)", true},
		{"outcome only", "metadata.log_type != \"\"\noutcome: $c = count(metadata.id)", true},
		{"match only", "metadata.log_type != \"\"\nmatch: metadata.log_type", true},
		{"match in value", `metadata.log_type = "match: foo"`, false},
		{"indented match", "  match: metadata.log_type", true},
		{"single-line aggregation", `metadata.log_type != "" match: metadata.log_type outcome: $c = count(metadata.id)`, true},
		{"no-space prefix not a section", `metadata.log_type = /rematch:x/`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAggregationQuery(tt.query); got != tt.want {
				t.Errorf("isAggregationQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}
