package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRulesCreateFlagChanged(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().Bool("alerting", true, "")
	cmd.Flags().String("run-frequency", "LIVE", "")

	if rulesCreateFlagChanged(cmd) {
		t.Fatal("unchanged rules-create flags reported changed")
	}
	if err := cmd.Flags().Set("enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if !rulesCreateFlagChanged(cmd) {
		t.Fatal("changed --enabled was not detected")
	}
}

// TestPullHasForwarders: forwarders is a pull target (symmetry with push/drift).
func TestPullHasForwarders(t *testing.T) {
	found := false
	for _, p := range pullOrder {
		if p.name == "forwarders" {
			found = true
		}
	}
	if !found {
		t.Error("forwarders is not a pull target")
	}
}
