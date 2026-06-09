package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSOARPlaybookCommandRegistered(t *testing.T) {
	soar := commandChild(rootCmd, "soar")
	if soar == nil {
		t.Fatal("soar command not registered")
	}
	playbook := commandChild(soar, "playbook")
	if playbook == nil {
		t.Fatal("soar playbook command not registered")
	}
	for _, name := range []string{
		"list",
		"delete",
		"validate",
		"components",
		"mold",
		"trigger",
		"test-cases",
		"run",
		"debug",
		"debug-step-data",
		"simulation-enrichment",
		"pending",
		"step",
		"rerun",
		"rerun-block",
		"summary",
		"results",
		"result",
		"python-logs",
	} {
		if commandChild(playbook, name) == nil {
			t.Fatalf("soar playbook %s command not registered", name)
		}
	}
	pending := commandChild(playbook, "pending")
	for _, name := range []string{"count", "list", "get"} {
		if commandChild(pending, name) == nil {
			t.Fatalf("soar playbook pending %s command not registered", name)
		}
	}
	step := commandChild(playbook, "step")
	for _, name := range []string{"get", "execute"} {
		if commandChild(step, name) == nil {
			t.Fatalf("soar playbook step %s command not registered", name)
		}
	}
}

func TestSOARPlaybookValidateSummarizesShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.json")
	body := strings.Join([]string{
		`{`,
		`  "name": "Case Workflow",`,
		`  "isEnabled": true,`,
		`  "categoryName": "Investigation",`,
		`  "templateName": null,`,
		`  "trigger": {"type": 1, "executionMode": "Automatic"},`,
		`  "steps": [`,
		`    {"identifier": "s1", "name": "Call API", "type": 0, "isAutomatic": true, "integration": "HTTP", "actionName": "Send Request", "parameters": []},`,
		`    {"identifier": "s2", "name": "Nested Block", "type": 5, "isAutomatic": false}`,
		`  ],`,
		`  "stepsRelations": [{"fromStep": "s1", "toStep": "s2"}]`,
		`}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARPlaybookValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate playbook: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"playbook: Case Workflow",
		"enabled: true",
		"trigger_type: 1",
		"steps: 2 (1 action, 1 block, 1 automatic, 1 manual)",
		"warnings: none",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestSOARPlaybookValidateRejectsUnsafeName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.json")
	if err := os.WriteFile(path, []byte(`{"name":"Bad: Name","templateName":null}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARPlaybookValidateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--file", path})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("validate accepted unsafe playbook name")
	}
	if !strings.Contains(err.Error(), "invalid playbook name") {
		t.Fatalf("error = %v, want invalid playbook name", err)
	}
}

func TestPlaybookRunBody(t *testing.T) {
	body, err := playbookRunBody(123, " Case Workflow ", "11111111-1111-1111-1111-111111111111", " group ", " alert ", false)
	if err != nil {
		t.Fatalf("playbookRunBody: %v", err)
	}
	want := map[string]any{
		"cyberCaseId":                          123,
		"shouldRunAutomatic":                   false,
		"wfName":                               "Case Workflow",
		"originalWorkflowDefinitionIdentifier": "11111111-1111-1111-1111-111111111111",
		"alertGroupIdentifier":                 "group",
		"alertIdentifier":                      "alert",
	}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, body[k], v)
		}
	}

	if _, err := playbookRunBody(123, "", "", "", "", true); err == nil {
		t.Fatal("playbookRunBody accepted missing playbook selector")
	}
}

func TestPlaybookBlockRunBody(t *testing.T) {
	dir := t.TempDir()
	inputsPath := filepath.Join(dir, "inputs.json")
	if err := os.WriteFile(inputsPath, []byte(`[{"fieldName":"CaseTags","value":"test"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := playbookBlockRunBody(123, "Nested Block", "", "", "", inputsPath)
	if err != nil {
		t.Fatalf("playbookBlockRunBody: %v", err)
	}
	if body["cyberCaseId"] != 123 {
		t.Fatalf("cyberCaseId = %#v, want 123", body["cyberCaseId"])
	}
	if body["wfName"] != "Nested Block" {
		t.Fatalf("wfName = %#v, want Nested Block", body["wfName"])
	}
	inputs, ok := body["inputParameters"].(json.RawMessage)
	if !ok {
		t.Fatalf("inputParameters type = %T, want json.RawMessage", body["inputParameters"])
	}
	if !strings.Contains(string(inputs), "CaseTags") {
		t.Fatalf("inputParameters = %s", inputs)
	}
}

func TestPlaybookDebugBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.json")
	raw := []byte(`{"name":"Debug Workflow","templateName":null}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	body, err := playbookDebugBody(path, 456)
	if err != nil {
		t.Fatalf("playbookDebugBody: %v", err)
	}
	if got := body["testCaseId"]; got != 456 {
		t.Fatalf("testCaseId = %#v, want 456", got)
	}
	workflow, ok := body["workflow"].(json.RawMessage)
	if !ok {
		t.Fatalf("workflow type = %T, want json.RawMessage", body["workflow"])
	}
	if string(workflow) != string(raw) {
		t.Fatalf("workflow = %s, want %s", workflow, raw)
	}
}

func TestPlaybookDebugHelperBodies(t *testing.T) {
	defaultTestCases := testCasesBody("", nil, 0, 50)
	if defaultTestCases["searchTerm"] != "" {
		t.Fatalf("default searchTerm = %#v, want empty string", defaultTestCases["searchTerm"])
	}
	envsDefault, ok := defaultTestCases["environments"].([]string)
	if !ok || len(envsDefault) != 0 {
		t.Fatalf("default environments = %#v, want empty slice", defaultTestCases["environments"])
	}

	testCases := testCasesBody(" sample ", []string{" Default ", ""}, 2, 25)
	for k, v := range map[string]any{
		"searchTerm":    "sample",
		"requestedPage": 2,
		"pageSize":      25,
	} {
		if testCases[k] != v {
			t.Fatalf("testCases[%s] = %#v, want %#v", k, testCases[k], v)
		}
	}
	envs, ok := testCases["environments"].([]string)
	if !ok || len(envs) != 1 || envs[0] != "Default" {
		t.Fatalf("environments = %#v, want [Default]", testCases["environments"])
	}

	step, err := debugStepDataBody(" step-uuid ", " alert-id ")
	if err != nil {
		t.Fatalf("debugStepDataBody: %v", err)
	}
	if step["stepOriginalIdentifier"] != "step-uuid" || step["alertIdentifier"] != "alert-id" {
		t.Fatalf("debug step body = %#v", step)
	}

	enrichment, err := simulationEnrichmentBody(321, " step-uuid ", " workflow-uuid ")
	if err != nil {
		t.Fatalf("simulationEnrichmentBody: %v", err)
	}
	for k, v := range map[string]any{
		"testCaseId":                 321,
		"originalStepIdentifier":     "step-uuid",
		"originalWorkflowIdentifier": "workflow-uuid",
	} {
		if enrichment[k] != v {
			t.Fatalf("enrichment[%s] = %#v, want %#v", k, enrichment[k], v)
		}
	}

	pending, err := pendingStepBody(123, " alert-group ", " workflow-uuid ")
	if err != nil {
		t.Fatalf("pendingStepBody: %v", err)
	}
	for k, v := range map[string]any{
		"caseId":                               123,
		"alertGroupIdentifier":                 "alert-group",
		"originalWorkflowDefinitionIdentifier": "workflow-uuid",
	} {
		if pending[k] != v {
			t.Fatalf("pending[%s] = %#v, want %#v", k, pending[k], v)
		}
	}
	if _, err := pendingStepBody(0, "", ""); err == nil {
		t.Fatal("pendingStepBody accepted missing case id")
	}
}

func TestWorkflowSummaryBody(t *testing.T) {
	body, err := workflowSummaryBody(123, " alert ", "22222222-2222-2222-2222-222222222222", true, false)
	if err != nil {
		t.Fatalf("workflowSummaryBody: %v", err)
	}
	for k, v := range map[string]any{
		"caseId":               123,
		"alertIdentifier":      "alert",
		"definitionIdentifier": "22222222-2222-2222-2222-222222222222",
		"shouldFetchSteps":     true,
		"collapseBlocks":       false,
	} {
		if body[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, body[k], v)
		}
	}
}

func TestActionResultsSummaryDoesNotPrintPayloads(t *testing.T) {
	raw := json.RawMessage(`[
	  {"status": "Completed", "message": "private message", "resultValue": "secret value", "pythonExecutionId": "py-1"},
	  {"status": "Failed", "resultJsonObject": "{\"token\":\"secret\"}"}
	]`)
	var out bytes.Buffer
	printActionResultsSummary(&out, raw)
	got := out.String()
	for _, want := range []string{
		"action_results: 2",
		"- Completed: 1",
		"- Failed: 1",
		"python_execution_ids: 1 present",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	for _, blocked := range []string{"private message", "secret value", "token"} {
		if strings.Contains(got, blocked) {
			t.Fatalf("summary leaked %q:\n%s", blocked, got)
		}
	}
}

func TestPrintPendingStepCount(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`12`),
		json.RawMessage(`{"count":12}`),
	} {
		var out bytes.Buffer
		printPendingStepCount(&out, raw)
		if got := out.String(); !strings.Contains(got, "pending_steps: 12") {
			t.Fatalf("count summary = %q, want pending_steps: 12", got)
		}
	}
}

func TestPrintFaultedSteps(t *testing.T) {
	raw := json.RawMessage(`[{"actionName":"Enrich Entity","status":"FAULTED",` +
		`"message":"Execution Failed.\n  Traceback (most recent call last):\n    line 1","` +
		`integrationInstanceName":"ExampleIntegration_1","logsExplorerUrl":"https://logs.example/x"}]`)

	var trunc bytes.Buffer
	printFaultedSteps(&trunc, raw, false)
	for _, want := range []string{
		"faulted (1):", "Enrich Entity", "FAULTED", "ExampleIntegration_1",
		"https://logs.example/x", "--show-errors",
	} {
		if !strings.Contains(trunc.String(), want) {
			t.Errorf("truncated output missing %q\n%s", want, trunc.String())
		}
	}
	// The truncated view collapses the newline-bearing traceback to one line.
	if strings.Count(strings.TrimSpace(trunc.String()), "\n") > 4 {
		t.Errorf("truncated view should be compact, got:\n%s", trunc.String())
	}

	var full bytes.Buffer
	printFaultedSteps(&full, raw, true)
	if !strings.Contains(full.String(), "Traceback (most recent call last):") {
		t.Errorf("--show-errors should print the full traceback, got:\n%s", full.String())
	}

	// No faulted steps → no output.
	var empty bytes.Buffer
	printFaultedSteps(&empty, json.RawMessage(`[]`), false)
	if empty.Len() != 0 {
		t.Errorf("empty faulted list should print nothing, got %q", empty.String())
	}
}

func TestPrintWorkflowSummaryCounts(t *testing.T) {
	raw := json.RawMessage(`{"completedSteps":[{},{}],"faultedSteps":[{"actionName":"A","status":"FAULTED"}],` +
		`"retryPendingSteps":[],"totalPendingActionSteps":1,"executionTimeInMs":42,"usedIntegrations":[{},{},{}]}`)
	var b bytes.Buffer
	printWorkflowSummary(&b, raw, false)
	out := b.String()
	for _, want := range []string{"completed: 2", "faulted: 1", "used_integrations: 3", "faulted (1):", "A"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n%s", want, out)
		}
	}
	// Must NOT dump the full step arrays.
	if strings.Contains(out, "completedSteps") || strings.Contains(out, "[{") {
		t.Errorf("summary should print counts, not dump arrays:\n%s", out)
	}
}
