package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

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
