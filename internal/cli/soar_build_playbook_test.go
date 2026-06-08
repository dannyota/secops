package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSOARBuildPlaybookCommandRegistered(t *testing.T) {
	soar := commandChild(rootCmd, "soar")
	if soar == nil {
		t.Fatal("soar command not registered")
	}
	if commandChild(soar, "build-playbook") == nil {
		t.Fatal("soar build-playbook command not registered")
	}
}

func TestSOARBuildPlaybookCommandWritesOutput(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.json")
	moldPath := filepath.Join(dir, "mold.json")
	outPath := filepath.Join(dir, "out", "playbook.json")
	base := strings.Join([]string{
		`{`,
		`  "name": "Base",`,
		`  "templateName": null,`,
		`  "trigger": {"cronSchedule": "old"},`,
		`  "steps": [{`,
		`    "id": 1,`,
		`    "identifier": "base-step",`,
		`    "name": "Placeholder",`,
		`    "integration": "Siemplify",`,
		`    "actionName": "Placeholder",`,
		`    "type": 0,`,
		`    "parameters": []`,
		`  }]`,
		`}`,
	}, "\n")
	mold := strings.Join([]string{
		`{`,
		`  "identifier": "mold-step",`,
		`  "name": "Send Request",`,
		`  "integration": "HTTP",`,
		`  "actionProvider": "HTTP",`,
		`  "actionName": "Send Request",`,
		`  "type": 0,`,
		`  "parameters": [{"name": "Target", "value": "example-uri", "type": 0}]`,
		`}`,
	}, "\n")
	if err := os.WriteFile(basePath, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moldPath, []byte(mold), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARBuildPlaybookCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"--base", basePath,
		"--cron", "0 9 * * *",
		"--name", "Built Playbook",
		"--replace-step", "Placeholder=" + moldPath,
		"--out", outPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute build-playbook: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Built Playbook" {
		t.Fatalf("name = %q", got["name"])
	}
	trigger := got["trigger"].(map[string]any)
	if trigger["cronSchedule"] != "0 9 * * *" {
		t.Fatalf("cronSchedule = %q", trigger["cronSchedule"])
	}
	step := got["steps"].([]any)[0].(map[string]any)
	if step["identifier"] != "base-step" {
		t.Fatalf("identifier = %q, want base-step", step["identifier"])
	}
	if step["integration"] != "HTTP" || step["actionName"] != "Send Request" {
		t.Fatalf("step action = (%q,%q)", step["integration"], step["actionName"])
	}
}

func TestParsePlaybookStepReplacementArg(t *testing.T) {
	match, path, err := parsePlaybookStepReplacementArg("Placeholder Step=steps/send.json")
	if err != nil {
		t.Fatal(err)
	}
	if match != "Placeholder Step" || path != "steps/send.json" {
		t.Fatalf("parse = (%q,%q)", match, path)
	}
	if _, _, err := parsePlaybookStepReplacementArg("missing-separator"); err == nil {
		t.Fatal("parse accepted missing separator")
	}
}
