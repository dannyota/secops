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
