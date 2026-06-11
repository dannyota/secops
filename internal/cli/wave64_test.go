package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `soar playbook step insert` is registered with its required flags and runs
// fully offline: splice a mold into a fixture playbook, write the result, and
// verify the new step's identity and rewired relation.
func TestPlaybookStepInsertOffline(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.json")
	mold := filepath.Join(dir, "mold.json")
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(base, []byte(`{"identifier":"wf-1","name":"demo",
		"steps":[{"identifier":"s-a","name":"Ping","instanceName":"Ping_1","type":0,"integration":"HTTP","actionName":"Ping","parameters":[]}],
		"stepsRelations":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mold, []byte(`{"name":"Close Case","type":0,"integration":"Siemplify",
		"actionName":"Close Case","parameters":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARPlaybookCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"step", "insert", "--file", base, "--mold", mold, "--after", "Ping", "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("insert: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var built struct {
		Steps []struct {
			Identifier   string `json:"identifier"`
			InstanceName string `json:"instanceName"`
			Workflow     string `json:"workflowIdentifier"`
		} `json:"steps"`
		Relations []struct {
			From string `json:"fromStep"`
			To   string `json:"toStep"`
		} `json:"stepsRelations"`
	}
	if err := json.Unmarshal(raw, &built); err != nil {
		t.Fatal(err)
	}
	if len(built.Steps) != 2 || built.Steps[1].InstanceName != "Close Case_1" {
		t.Fatalf("steps = %+v", built.Steps)
	}
	if built.Steps[1].Workflow != "wf-1" || len(built.Steps[1].Identifier) != 36 {
		t.Errorf("identity = %+v", built.Steps[1])
	}
	if len(built.Relations) != 1 || built.Relations[0].From != "s-a" || built.Relations[0].To != built.Steps[1].Identifier {
		t.Errorf("relations = %+v", built.Relations)
	}
}

func TestPlaybookStepInsertFlagValidation(t *testing.T) {
	cmd := newSOARPlaybookCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"step", "insert"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("missing required flags must error, got %v", err)
	}
}
