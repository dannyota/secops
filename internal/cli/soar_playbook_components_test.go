package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPlaybookComponentsCommandRegistered(t *testing.T) {
	cmd := newSOARPlaybookComponentsCmd()
	for _, name := range []string{"integrations", "actions", "jobs", "connectors"} {
		if commandChild(cmd, name) == nil {
			t.Fatalf("soar playbook components %s command not registered", name)
		}
	}
}

// TestActionParameterSchemaCaptured locks FR-31b: an action row carries the full
// parameter schema (name/type/mandatory/default/optionalValues/description), so a
// step can be authored from --json output, not just a parameter count.
func TestActionParameterSchemaCaptured(t *testing.T) {
	raw := json.RawMessage(`{"actions":[{
	  "name":"Enrich Entity","scriptResultName":"r",
	  "parameters":[
	    {"name":"Key","type":"STRING","isMandatory":true,"defaultValue":"","description":"the lookup key"},
	    {"name":"Mode","type":"DDL","isMandatory":false,"defaultValue":"fast","optionalValues":["fast","full"]}
	  ]}]}`)
	rows := summarizeIntegrationActions("Example", raw)
	if len(rows) != 1 || len(rows[0].Parameters) != 2 {
		t.Fatalf("expected 1 action with 2 parameters, got %#v", rows)
	}
	key := rows[0].Parameters[0]
	if key.Name != "Key" || key.Type != "STRING" || !key.Mandatory || key.Description != "the lookup key" {
		t.Fatalf("first param schema not captured: %#v", key)
	}
	mode := rows[0].Parameters[1]
	if mode.Mandatory || mode.DefaultValue != "fast" || !reflect.DeepEqual(mode.OptionalValues, []string{"fast", "full"}) {
		t.Fatalf("second param schema not captured: %#v", mode)
	}
}

// TestWrapActionsEnvelope verifies per-action full bodies are re-wrapped into the
// {"actions":[…]} envelope the summarizer walks.
func TestWrapActionsEnvelope(t *testing.T) {
	raws := []json.RawMessage{
		json.RawMessage(`{"name":"A","parameters":[{"name":"P","isMandatory":true}]}`),
		json.RawMessage(`{"name":"B"}`),
		nil, // skipped
	}
	rows := summarizeIntegrationActions("X", wrapActionsEnvelope(raws))
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows from re-wrapped bodies, got %d", len(rows))
	}
}

// TestActionParameterModernShape locks the modern action-GET parameter shape
// (displayName/mandatory) — distinct from the legacy details shape (name/isMandatory).
func TestActionParameterModernShape(t *testing.T) {
	raw := json.RawMessage(`{"actions":[{
	  "displayName":"Ask Gemini","scriptResultName":"r","enabled":true,
	  "parameters":[
	    {"displayName":"Prompt","type":"STRING","mandatory":true,"defaultValue":"","description":"the prompt"},
	    {"displayName":"Mode","type":"DDL","mandatory":false,"defaultValue":"fast","optionalValues":["fast","full"]}
	  ]}]}`)
	rows := summarizeIntegrationActions("X", raw)
	if len(rows) != 1 || len(rows[0].Parameters) != 2 {
		t.Fatalf("modern shape not parsed: %#v", rows)
	}
	if p := rows[0].Parameters[0]; p.Name != "Prompt" || !p.Mandatory || p.Type != "STRING" {
		t.Fatalf("modern param0 not captured: %#v", p)
	}
	if !reflect.DeepEqual(rows[0].MandatoryParameters, []string{"Prompt"}) {
		t.Errorf("mandatory list = %#v", rows[0].MandatoryParameters)
	}
}

func TestSummarizeIntegrationActions(t *testing.T) {
	raw := json.RawMessage(`{
	  "actions": [
	    {
	      "id": 10,
	      "name": "Lookup Entity",
	      "description": "Finds data",
	      "isEnabled": true,
	      "hasJsonResult": true,
	      "isAsync": false,
	      "scriptResultName": "lookup_result",
	      "actionType": 1,
	      "parameters": [
	        {"name": "Entity", "isMandatory": true},
	        {"name": "Limit", "isMandatory": false}
	      ],
	      "script": "print('do not summarize')"
	    }
	  ],
	  "metadata": {
	    "integrationSupportedActions": [
	      {"id": 11, "name": "Ping", "description": "Connectivity"}
	    ]
	  }
	}`)
	rows := summarizeIntegrationActions("Example", raw)
	if len(rows) != 2 {
		t.Fatalf("rows = %#v, want 2", rows)
	}
	first := rows[0]
	if first.Name != "Lookup Entity" {
		t.Fatalf("first action = %#v", first)
	}
	if first.ParameterCount != 2 {
		t.Fatalf("ParameterCount = %d, want 2", first.ParameterCount)
	}
	if len(first.MandatoryParameters) != 1 || first.MandatoryParameters[0] != "Entity" {
		t.Fatalf("MandatoryParameters = %#v", first.MandatoryParameters)
	}
	if first.HasJSONResult == nil || !*first.HasJSONResult {
		t.Fatalf("HasJSONResult = %#v, want true", first.HasJSONResult)
	}
	if strings.Contains(first.Description, "do not summarize") {
		t.Fatalf("action summary leaked script body: %#v", first)
	}
}

func TestFilterActionRows(t *testing.T) {
	rows := []playbookActionRow{
		{Name: "Lookup Entity", Description: "Finds data"},
		{Name: "Ping", Description: "Connectivity"},
	}
	filtered := filterActionRows(rows, "connect")
	if len(filtered) != 1 || filtered[0].Name != "Ping" {
		t.Fatalf("filtered = %#v, want Ping", filtered)
	}
}

func TestSummarizeIntegrationActionsIgnoresNonActionObjects(t *testing.T) {
	raw := json.RawMessage(`{
	  "name": "Example Integration",
	  "isEnabled": true,
	  "parameters": [{"name": "ApiRoot"}],
	  "actions": [{"name": "Ping", "parameters": []}]
	}`)
	rows := summarizeIntegrationActions("Example", raw)
	if len(rows) != 1 || rows[0].Name != "Ping" {
		t.Fatalf("rows = %#v, want only Ping action", rows)
	}
}
