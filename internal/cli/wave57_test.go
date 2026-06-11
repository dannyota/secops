package cli

import (
	"strings"
	"testing"
)

// `alerts investigate` is registered, takes exactly one positional alert id,
// and refuses to start a generation in read-only mode BEFORE touching any
// client/credentials — while --latest stays available as the read-only path.
func TestAlertsInvestigateCommand(t *testing.T) {
	root := newAlertsCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "investigate" {
			found = true
			if c.Flags().Lookup("latest") == nil {
				t.Error("investigate must have --latest")
			}
			if err := c.Args(c, []string{}); err == nil {
				t.Error("investigate must require an alert id")
			}
			if err := c.Args(c, []string{"a", "b"}); err == nil {
				t.Error("investigate must reject extra args")
			}
		}
	}
	if !found {
		t.Fatal("alerts investigate not registered")
	}
}

func TestAlertsInvestigateReadOnlyRefusal(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "1")
	root := newAlertsCmd()
	root.SetArgs([]string{"investigate", "de_00000000-0000-0000-0000-000000000000"})
	root.SilenceUsage, root.SilenceErrors = true, true
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("read-only mode must refuse the generation before any API call, got %v", err)
	}
}
