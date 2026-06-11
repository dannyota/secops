package cli

import (
	"strings"
	"testing"
)

// The authoring `update` verb patches by numeric id and needs at least one
// field to change; with neither --script nor --description it must refuse
// (offline, before any API call).
func TestAuthoringUpdateRequiresAField(t *testing.T) {
	cmd := newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update", "--integration", "HTTP", "--id", "42", "--yes"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("update with no field flags must refuse, got %v", err)
	}

	// Missing required flags (--integration / --id).
	cmd = newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("update without flags must error, got %v", err)
	}
}

func TestCollectionFor(t *testing.T) {
	if collectionFor("action") != "actions" || collectionFor("job") != "jobs" {
		t.Error("collectionFor must map action->actions, job->jobs")
	}
}
