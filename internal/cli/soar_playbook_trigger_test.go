package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSOARPlaybookTriggerSet(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "playbook.json")
	conditions := filepath.Join(dir, "conditions.json")
	out := filepath.Join(dir, "out.json")
	body := `{
	  "name": "Trigger Edit",
	  "templateName": null,
	  "isEnabled": false,
	  "trigger": {
	    "id": 1,
	    "type": 2,
	    "executionMode": "Manual",
	    "unknown": "preserve"
	  },
	  "steps": []
	}`
	if err := os.WriteFile(in, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conditions, []byte(`[{"field":"Tag","operator":"Equals","value":"Smoke"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARPlaybookTriggerSetCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--file", in,
		"--out", out,
		"--enabled", "true",
		"--trigger-enabled", "false",
		"--type", "8",
		"--execution-mode", "Automatic",
		"--cron", "0 8 * * *",
		"--conditions", conditions,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trigger set: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["isEnabled"] != true {
		t.Fatalf("isEnabled = %#v, want true", got["isEnabled"])
	}
	trigger := got["trigger"].(map[string]any)
	for k, want := range map[string]any{
		"id":            "1",
		"type":          float64(8),
		"executionMode": "Automatic",
		"cronSchedule":  "0 8 * * *",
		"isEnabled":     false,
		"unknown":       "preserve",
	} {
		if trigger[k] != want {
			t.Fatalf("trigger[%s] = %#v, want %#v", k, trigger[k], want)
		}
	}
	if _, ok := trigger["conditions"].([]any); !ok {
		t.Fatalf("conditions = %#v, want array", trigger["conditions"])
	}
}

func TestOptionalTriggerScalar(t *testing.T) {
	v, ok, err := optionalTriggerScalar("8")
	if err != nil || !ok {
		t.Fatalf("optionalTriggerScalar number = (%#v,%t,%v)", v, ok, err)
	}
	if n, ok := v.(json.Number); !ok || n.String() != "8" {
		t.Fatalf("number = %#v, want json.Number(8)", v)
	}
	v, ok, err = optionalTriggerScalar("Case")
	if err != nil || !ok || v != "Case" {
		t.Fatalf("optionalTriggerScalar string = (%#v,%t,%v), want Case", v, ok, err)
	}
}
