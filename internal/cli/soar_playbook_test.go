package cli

import (
	"bytes"
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
	for _, name := range []string{"list", "validate"} {
		if commandChild(playbook, name) == nil {
			t.Fatalf("soar playbook %s command not registered", name)
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
