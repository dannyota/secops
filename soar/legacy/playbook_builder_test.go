package legacy

import (
	"bytes"
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
	if !strings.Contains(err.Error(), "want action step") {
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

const insertBasePlaybook = `{
  "identifier": "wf-1",
  "name": "base",
  "customField": {"keep": true},
  "steps": [
    {"identifier": "s-cond", "name": "Check", "instanceName": "Check_1", "type": 1, "integration": "Flow", "actionName": "IfFlowCondition", "parameters": []},
    {"identifier": "s-a", "name": "Ping", "instanceName": "Ping_1", "type": 0, "integration": "HTTP", "actionName": "Ping", "parameters": [], "timeoutSeconds": 9007199254740993, "creationTimeUnixTimeInMs": 9007199254740995},
    {"identifier": "s-b", "name": "Close", "instanceName": "Close_1", "type": 0, "integration": "Siemplify", "actionName": "Close Case", "parameters": []}
  ],
  "stepsRelations": [
    {"fromStep": "s-cond", "toStep": "s-a", "condition": "1"},
    {"fromStep": "s-cond", "toStep": "s-b", "condition": "2"},
    {"fromStep": "s-a", "toStep": "s-b", "condition": ""}
  ]
}`

const insertMold = `{
  "identifier": "mold-id", "originalStepIdentifier": "mold-orig",
  "workflowIdentifier": "other-wf", "parentStepContainerId": "container-9",
  "name": "Ping", "instanceName": "Ping_1", "type": 0,
  "integration": "HTTP", "actionName": "Ping",
  "parameters": [{"name": "ScriptName", "value": "Ping"}],
  "moldExtra": "kept"
}`

func decodeInserted(t *testing.T, out json.RawMessage) (map[string]any, []any, []any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	steps, _ := got["steps"].([]any)
	rels, _ := got["stepsRelations"].([]any)
	return got, steps, rels
}

func relOf(t *testing.T, rels []any, from string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range rels {
		rel := raw.(map[string]any)
		if rel["fromStep"] == from {
			out = append(out, rel)
		}
	}
	return out
}

// Splicing into the middle of an edge: anchor keeps its condition toward the
// new step, the new step flows on to the old successor, fresh identity is
// minted, and unknown fields on both sides survive byte-meaningful.
func TestInsertActionStepSplicesEdge(t *testing.T) {
	out, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Ping", NewIdentifier: "s-new",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, steps, rels := decodeInserted(t, out)
	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(steps))
	}
	inserted := steps[3].(map[string]any)
	if inserted["identifier"] != "s-new" || inserted["originalStepIdentifier"] != "s-new" {
		t.Errorf("identity = %v / %v, want fresh s-new", inserted["identifier"], inserted["originalStepIdentifier"])
	}
	if inserted["workflowIdentifier"] != "wf-1" {
		t.Errorf("workflowIdentifier = %v, want wf-1", inserted["workflowIdentifier"])
	}
	if _, leaked := inserted["parentStepContainerId"]; leaked {
		t.Error("container placement must not follow the mold")
	}
	if inserted["moldExtra"] != "kept" {
		t.Errorf("unknown mold field lost: %v", inserted["moldExtra"])
	}
	if inserted["instanceName"] != "Ping_2" {
		t.Errorf("instanceName = %v, want unique Ping_2", inserted["instanceName"])
	}
	// s-a's single outgoing edge now routes through the new step.
	if rel := relOf(t, rels, "s-a"); len(rel) != 1 || rel[0]["toStep"] != "s-new" || rel[0]["condition"] != "" {
		t.Errorf("anchor relation = %v", rel)
	}
	if rel := relOf(t, rels, "s-new"); len(rel) != 1 || rel[0]["toStep"] != "s-b" {
		t.Errorf("successor relation = %v", rel)
	}
	if got["customField"].(map[string]any)["keep"] != true {
		t.Error("unknown playbook field lost")
	}
	// int64 fields must not round-trip through float64 — both a plain field
	// and one the save coercion stringifies (creationTimeUnixTimeInMs).
	if !bytes.Contains(out, []byte("9007199254740993")) {
		t.Error("int64 step field corrupted")
	}
	if !bytes.Contains(out, []byte(`"9007199254740995"`)) {
		t.Error("coerced int64 field corrupted or not stringified")
	}
}

// A condition anchor needs Branch; the selected branch keeps its condition.
func TestInsertActionStepBranchSelection(t *testing.T) {
	if _, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Check",
	}); err == nil || !strings.Contains(err.Error(), "Branch") {
		t.Errorf("ambiguous anchor must demand Branch, got %v", err)
	}
	out, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Check", Branch: "2", NewIdentifier: "s-new",
	})
	if err != nil {
		t.Fatalf("insert on branch: %v", err)
	}
	_, _, rels := decodeInserted(t, out)
	var toNew map[string]any
	for _, rel := range relOf(t, rels, "s-cond") {
		if rel["toStep"] == "s-new" {
			toNew = rel
		}
	}
	if toNew == nil || toNew["condition"] != "2" {
		t.Fatalf("branch-2 edge not rewired through the new step: %v", relOf(t, rels, "s-cond"))
	}
	if rel := relOf(t, rels, "s-new"); len(rel) != 1 || rel[0]["toStep"] != "s-b" {
		t.Errorf("successor relation = %v", rel)
	}
	if _, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Check", Branch: "9",
	}); err == nil || !strings.Contains(err.Error(), `"9"`) {
		t.Errorf("unknown branch must error with the available conditions, got %v", err)
	}
}

// A tail anchor (no outgoing relation) appends the new step after it.
func TestInsertActionStepTail(t *testing.T) {
	out, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Close", NewIdentifier: "s-new",
	})
	if err != nil {
		t.Fatalf("tail insert: %v", err)
	}
	_, _, rels := decodeInserted(t, out)
	if rel := relOf(t, rels, "s-b"); len(rel) != 1 || rel[0]["toStep"] != "s-new" || rel[0]["condition"] != "" {
		t.Errorf("tail relation = %v", rel)
	}
	if rel := relOf(t, rels, "s-new"); len(rel) != 0 {
		t.Errorf("new tail must have no successor, got %v", rel)
	}
}

// Without NewIdentifier a real UUID is minted.
func TestInsertActionStepMintsUUID(t *testing.T) {
	out, err := InsertActionStep(json.RawMessage(insertBasePlaybook), PlaybookStepInsertOptions{
		Mold: json.RawMessage(insertMold), After: "Close",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, steps, _ := decodeInserted(t, out)
	id, _ := steps[3].(map[string]any)["identifier"].(string)
	if len(id) != 36 || id == "mold-id" {
		t.Errorf("identifier = %q, want a fresh UUID", id)
	}
}
