package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowStepRequestBody(t *testing.T) {
	body, err := workflowStepRequestBody(123, " alert ", " workflow-uuid ", " step-uuid ", " block-uuid ", 2, 3)
	if err != nil {
		t.Fatalf("workflowStepRequestBody: %v", err)
	}
	for k, v := range map[string]any{
		"caseId":                      123,
		"alertIdentifier":             "alert",
		"workflowIdentifier":          "workflow-uuid",
		"stepIdentifier":              "step-uuid",
		"blockStepId":                 "block-uuid",
		"loopIteration":               2,
		"parentWorkflowLoopIteration": 3,
	} {
		if body[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, body[k], v)
		}
	}
	if _, err := workflowStepRequestBody(0, "", "workflow", "step", "", -1, -1); err == nil {
		t.Fatal("workflowStepRequestBody accepted missing case id")
	}
	if _, err := workflowStepRequestBody(1, "", "", "step", "", -1, -1); err == nil {
		t.Fatal("workflowStepRequestBody accepted missing workflow")
	}
	if _, err := workflowStepRequestBody(1, "", "workflow", "", "", -1, -1); err == nil {
		t.Fatal("workflowStepRequestBody accepted missing step")
	}
}

func TestReadWorkflowStepInstanceFileSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "step.json")
	raw := `{
	  "caseId": 123,
	  "workflowInstanceIdentifier": 456,
	  "workflowIdentifier": "workflow-uuid",
	  "stepInstanceIdentifier": "step-instance",
	  "identifier": "step-id",
	  "originalStepIdentifier": "original-step",
	  "status": "Pending",
	  "allowedToExecute": true,
	  "parameters": [{"name":"token"}],
	  "message": "private message",
	  "resultValue": "secret result",
	  "jsonResultObject": "{\"token\":\"secret\"}",
	  "resultEntities": [{"identifier":"entity"}]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	body, summary, err := readWorkflowStepInstanceFile(path)
	if err != nil {
		t.Fatalf("readWorkflowStepInstanceFile: %v", err)
	}
	if len(body) == 0 || summary.CaseID != 123 || summary.WorkflowInstanceIdentifier != 456 {
		t.Fatalf("summary = %#v, body len = %d", summary, len(body))
	}
	if summary.AllowedToExecute == nil || !*summary.AllowedToExecute {
		t.Fatalf("allowed_to_execute = %#v, want true", summary.AllowedToExecute)
	}
	if summary.ParameterCount != 1 || !summary.HasMessage || !summary.HasResultValue || !summary.HasJSONResult || !summary.HasResultEntities {
		t.Fatalf("summary flags = %#v", summary)
	}
}

func TestWorkflowStepSummaryDoesNotPrintPayloads(t *testing.T) {
	summary := workflowStepInstanceSummary{
		File:                   "step.json",
		CaseID:                 123,
		WorkflowIdentifier:     "workflow-uuid",
		StepInstanceIdentifier: "step-instance",
		Status:                 "Pending",
		ParameterCount:         1,
		HasMessage:             true,
		HasResultValue:         true,
		HasJSONResult:          true,
		HasResultEntities:      true,
	}
	var out bytes.Buffer
	printWorkflowStepInstanceSummary(&out, summary)
	got := out.String()
	for _, want := range []string{
		"workflow_step:",
		"case_id: 123",
		"workflow_identifier: workflow-uuid",
		"message: true",
		"json_result: true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	for _, blocked := range []string{"private message", "secret result", "token"} {
		if strings.Contains(got, blocked) {
			t.Fatalf("summary leaked %q:\n%s", blocked, got)
		}
	}
}

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
