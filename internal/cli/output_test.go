package cli

import (
	"strings"
	"testing"
)

// resetOutputFlags restores the global format state a test mutates.
func resetOutputFlags(t *testing.T) {
	t.Helper()
	prevJSON, prevFormat := jsonOut, outputFormat
	t.Cleanup(func() { jsonOut, outputFormat = prevJSON, prevFormat })
}

func TestEffectiveFormatPrecedence(t *testing.T) {
	resetOutputFlags(t)

	cases := []struct {
		name   string
		local  string
		global string
		json   bool
		want   string
	}{
		{"local wins over global", "csv", "table", true, "csv"},
		{"global wins over json", "", "csv", true, "csv"},
		{"json fallback", "", "", true, "json"},
		{"no preference", "", "", false, ""},
	}
	for _, tc := range cases {
		outputFormat, jsonOut = tc.global, tc.json
		if got := effectiveFormat(tc.local); got != tc.want {
			t.Errorf("%s: effectiveFormat(%q) = %q, want %q", tc.name, tc.local, got, tc.want)
		}
	}
}

func TestNormalizeOutputFlags(t *testing.T) {
	resetOutputFlags(t)

	outputFormat, jsonOut = "json", false
	if err := normalizeOutputFlags(); err != nil {
		t.Fatal(err)
	}
	if !jsonOut {
		t.Error("--output json did not set jsonOut")
	}

	outputFormat = "yaml"
	if err := normalizeOutputFlags(); err == nil {
		t.Error("unknown --output value accepted")
	}
}

func TestPrintCSVTo(t *testing.T) {
	var b strings.Builder
	err := printCSVTo(&b, []string{"a", "b"}, [][]string{{"1", "x,y"}, {"2", `q"uote`}})
	if err != nil {
		t.Fatal(err)
	}
	want := "a,b\n1,\"x,y\"\n2,\"q\"\"uote\"\n"
	if b.String() != want {
		t.Errorf("printCSVTo = %q, want %q", b.String(), want)
	}
}
