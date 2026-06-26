package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestSoarCaseIntID parses the integer case id from a resource name or bare id,
// the form the legacy bulk-close endpoint takes.
func TestSoarCaseIntID(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"projects/p/locations/l/instances/i/cases/1234", 1234, false},
		{"1234", 1234, false},
		{" 1234 ", 1234, false},
		{"cases/not-a-number", 0, true},
		{"", 0, true},
	}
	for _, tc := range cases {
		got, err := soarCaseIntID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("soarCaseIntID(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("soarCaseIntID(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

// TestCasesUnifiedCommand asserts the single top-level `cases` command exposes the
// working SOAR case verbs plus the uuid→id bridge, all visible — a case is one
// record, one command. The dead Chronicle-host list/get/search verbs are gone.
func TestCasesUnifiedCommand(t *testing.T) {
	cases := newCasesCmd()
	have := map[string]*cobra.Command{}
	for _, sub := range cases.Commands() {
		have[sub.Name()] = sub
	}
	for _, name := range []string{"list", "get", "close", "assign", "soar-id"} {
		sub, ok := have[name]
		if !ok {
			t.Errorf("cases is missing the %q verb", name)
			continue
		}
		if sub.Hidden {
			t.Errorf("cases %s: Hidden = true, want a visible working verb", name)
		}
	}
	// `search` was a dead Chronicle-host verb; it must no longer exist.
	if _, ok := have["search"]; ok {
		t.Errorf("cases still exposes the removed `search` verb")
	}
}
