package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/soar"
)

func TestPlaybookComponentsCommandRegistered(t *testing.T) {
	cmd := newSOARPlaybookComponentsCmd()
	for _, name := range []string{"integrations", "actions", "jobs", "connectors"} {
		if commandChild(cmd, name) == nil {
			t.Fatalf("soar playbook components %s command not registered", name)
		}
	}
}

func TestIntegrationFullDetailsBodyUsesProductionIdentifier(t *testing.T) {
	body := integrationFullDetailsBody(soar.Integration{
		Identifier:     "Installed__123",
		ProdIdentifier: "Base",
		Custom:         true,
		Certified:      true,
	})
	for k, v := range map[string]any{
		"integrationIdentifier": "Base",
		"isCustom":              true,
		"isCertified":           true,
	} {
		if body[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, body[k], v)
		}
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
