package legacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPlaybookFromMoldsSetsCronAndReplacesStep(t *testing.T) {
	base := Playbook(`{
	  "name": "Base Playbook",
	  "templateName": null,
	  "trigger": {"type": 8, "cronSchedule": "old"},
	  "steps": [{
	    "id": 10,
	    "identifier": "base-step",
	    "originalStepIdentifier": "base-original",
	    "workflowIdentifier": "base-workflow",
	    "name": "Placeholder Step",
	    "integration": "Siemplify",
	    "actionName": "Placeholder",
	    "type": 0,
	    "parameters": [],
	    "additionalProperties": {"x": "1"}
	  }],
	  "stepsRelations": [{"fromStep": "base-step", "toStep": "base-step", "condition": "ok"}]
	}`)
	mold := json.RawMessage(`{
	  "id": 99,
	  "identifier": "mold-step",
	  "originalStepIdentifier": "mold-original",
	  "workflowIdentifier": "mold-workflow",
	  "name": "Send Request",
	  "integration": "HTTP",
	  "actionProvider": "HTTP",
	  "actionName": "Send Request",
	  "description": "Send an outbound request",
	  "type": 0,
	  "parameters": [{"name": "URL", "value": "example-uri", "type": 0}],
	  "additionalProperties": {"x": "999"}
	}`)

	built, err := BuildPlaybookFromMolds(base, PlaybookBuildOptions{
		Name:         "Scheduled Response",
		CronSchedule: "0 8 * * *",
		StepReplacements: []PlaybookStepReplacement{{
			Match: "Placeholder Step",
			Mold:  mold,
		}},
	})
	if err != nil {
		t.Fatalf("BuildPlaybookFromMolds: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(built, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Scheduled Response" {
		t.Fatalf("name = %q", got["name"])
	}
	if got["templateName"] != "" {
		t.Fatalf("templateName = %#v, want empty string", got["templateName"])
	}
	trigger := got["trigger"].(map[string]any)
	if trigger["cronSchedule"] != "0 8 * * *" {
		t.Fatalf("cronSchedule = %q", trigger["cronSchedule"])
	}
	step := got["steps"].([]any)[0].(map[string]any)
	if step["integration"] != "HTTP" || step["actionName"] != "Send Request" {
		t.Fatalf("step action = (%q,%q)", step["integration"], step["actionName"])
	}
	if step["identifier"] != "base-step" || step["originalStepIdentifier"] != "base-original" {
		t.Fatalf("step identity was not preserved: %#v", step)
	}
	if step["id"] != "10" {
		t.Fatalf("step id = %#v, want save-ready string", step["id"])
	}
	props := step["additionalProperties"].(map[string]any)
	if props["x"] != "1" {
		t.Fatalf("additionalProperties = %#v, want base layout preserved", props)
	}
	params := step["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("parameters = %#v", params)
	}
	relations := got["stepsRelations"].([]any)
	if len(relations) != 1 {
		t.Fatalf("stepsRelations = %#v", relations)
	}
}

func TestExtractActionStepMold(t *testing.T) {
	playbook := Playbook(`{
	  "name": "Source",
	  "templateName": null,
	  "steps": [
	    {"identifier": "s1", "name": "Nested Block", "type": 5},
	    {
	      "identifier": "s2",
	      "originalStepIdentifier": "orig-s2",
	      "name": "Lookup",
	      "type": 0,
	      "integration": "Example",
	      "actionName": "Lookup Entity",
	      "parameters": [{"name": "Entity"}]
	    }
	  ]
	}`)
	mold, err := ExtractActionStepMold(playbook, "Lookup")
	if err != nil {
		t.Fatalf("ExtractActionStepMold: %v", err)
	}
	var step map[string]any
	if err := json.Unmarshal(mold, &step); err != nil {
		t.Fatal(err)
	}
	if step["integration"] != "Example" || step["actionName"] != "Lookup Entity" {
		t.Fatalf("step action = (%q,%q)", step["integration"], step["actionName"])
	}
}

func TestPatchPlaybookTrigger(t *testing.T) {
	enabled := true
	triggerEnabled := false
	cron := "0 9 * * *"
	patched, err := PatchPlaybookTrigger(Playbook(`{
	  "name": "Base",
	  "templateName": null,
	  "isEnabled": false,
	  "trigger": {
	    "id": 2,
	    "type": 1,
	    "executionMode": "Manual",
	    "cronSchedule": "old",
	    "unknownField": "preserved"
	  },
	  "steps": []
	}`), PlaybookTriggerPatchOptions{
		PlaybookEnabled:    &enabled,
		TriggerEnabled:     &triggerEnabled,
		Type:               json.Number("8"),
		ExecutionMode:      "Automatic",
		CronSchedule:       &cron,
		Conditions:         json.RawMessage(`[{"field":"AlertType","operator":"Equals","value":"Rule"}]`),
		ReactionConditions: json.RawMessage(`{"operator":"AND","conditions":[]}`),
	})
	if err != nil {
		t.Fatalf("PatchPlaybookTrigger: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatal(err)
	}
	if got["isEnabled"] != true {
		t.Fatalf("isEnabled = %#v, want true", got["isEnabled"])
	}
	trigger := got["trigger"].(map[string]any)
	for k, want := range map[string]any{
		"id":            "2",
		"type":          float64(8),
		"executionMode": "Automatic",
		"cronSchedule":  cron,
		"unknownField":  "preserved",
		"isEnabled":     false,
	} {
		if trigger[k] != want {
			t.Fatalf("trigger[%s] = %#v, want %#v", k, trigger[k], want)
		}
	}
	if _, ok := trigger["conditions"].([]any); !ok {
		t.Fatalf("conditions = %#v, want array", trigger["conditions"])
	}
	if _, ok := trigger["reactionConditions"].(map[string]any); !ok {
		t.Fatalf("reactionConditions = %#v, want object", trigger["reactionConditions"])
	}
}

func TestBuildPlaybookFromMoldsRejectsUnwiredStepMold(t *testing.T) {
	base := Playbook(`{"name":"Base","trigger":{},"steps":[{"name":"Placeholder"}]}`)
	_, err := BuildPlaybookFromMolds(base, PlaybookBuildOptions{
		StepReplacements: []PlaybookStepReplacement{{
			Match: "Placeholder",
			Mold:  json.RawMessage(`{"name":"Not Action","type":4,"parameters":[]}`),
		}},
	})
	if err == nil {
		t.Fatal("BuildPlaybookFromMolds accepted a non-action mold")
	}
	if !strings.Contains(err.Error(), "want action step type 0") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPlaybookFromMoldsRejectsMissingBaseStep(t *testing.T) {
	base := Playbook(`{"name":"Base","trigger":{},"steps":[{"name":"Other"}]}`)
	_, err := BuildPlaybookFromMolds(base, PlaybookBuildOptions{
		StepReplacements: []PlaybookStepReplacement{{
			Match: "Placeholder",
			Mold:  json.RawMessage(`{"name":"Action","integration":"HTTP","actionName":"Send","type":0,"parameters":[]}`),
		}},
	})
	if err == nil {
		t.Fatal("BuildPlaybookFromMolds accepted a missing base step")
	}
	if !strings.Contains(err.Error(), "no base playbook step matches") {
		t.Fatalf("error = %v", err)
	}
}
