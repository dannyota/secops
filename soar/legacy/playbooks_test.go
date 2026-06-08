package legacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreparePlaybookForSaveCoercesTypes(t *testing.T) {
	body := json.RawMessage(`{
	  "name": "Good_Name-1",
	  "id": 1,
	  "templateName": null,
	  "trigger": {"id": 2, "version": 3},
	  "steps": [{"id": 4, "priority": 5}]
	}`)

	prepared, err := preparePlaybookForSave(body)
	if err != nil {
		t.Fatalf("preparePlaybookForSave: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(prepared, &got); err != nil {
		t.Fatalf("unmarshal prepared playbook: %v", err)
	}
	if got["id"] != "1" {
		t.Errorf("id = %#v, want string 1", got["id"])
	}
	if got["templateName"] != "" {
		t.Errorf("templateName = %#v, want empty string", got["templateName"])
	}
	trigger := got["trigger"].(map[string]any)
	if trigger["id"] != "2" || trigger["version"] != "3" {
		t.Errorf("trigger = %#v, want string id/version", trigger)
	}
	step := got["steps"].([]any)[0].(map[string]any)
	if step["id"] != "4" || step["priority"] != "5" {
		t.Errorf("step = %#v, want string id/priority", step)
	}
}

func TestValidatePlaybookForSaveRejectsUnsafeName(t *testing.T) {
	err := ValidatePlaybookForSave(json.RawMessage(`{"name":"Bad: Name","templateName":null}`))
	if err == nil {
		t.Fatal("ValidatePlaybookForSave accepted unsafe name")
	}
	if !strings.Contains(err.Error(), "invalid playbook name") {
		t.Fatalf("error = %v, want invalid playbook name", err)
	}
}
