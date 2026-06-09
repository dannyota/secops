package cli

import (
	"bytes"
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
