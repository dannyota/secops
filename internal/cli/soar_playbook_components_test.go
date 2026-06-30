package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

// TestActionParameterSchemaCaptured verifies an action row carries the full
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

// The components palette covers every designer Step Selection tab, `actions`
// no longer requires --integration (omitting it = the all-integration
// catalog), and `triggers` works fully offline.
func TestComponentsPaletteCommands(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	want := map[string]bool{
		"integrations": false, "actions": false, "jobs": false,
		"connectors": false, "usage": false, "flow": false,
		"triggers": false, "blocks": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
		if c.Name() == "actions" {
			if f := c.Flags().Lookup("integration"); f == nil {
				t.Error("actions must keep --integration")
			} else if req, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(req) > 0 && req[0] == "true" {
				t.Error("actions --integration must be optional (omit = all-integration catalog)")
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("components %s not registered", name)
		}
	}
}

func TestComponentsTriggersOffline(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"triggers"})
	if err := root.Execute(); err != nil {
		t.Fatalf("triggers must run offline: %v", err)
	}
	for _, token := range []string{"ALL", "CASE_DATA", "GET_INPUTS"} {
		if !strings.Contains(out.String(), token) {
			t.Errorf("triggers output missing %s", token)
		}
	}
}

func TestUsageFlagValidation(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	root.SilenceUsage, root.SilenceErrors = true, true
	root.SetArgs([]string{"usage"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("usage without flags must demand --action-id or --action, got %v", err)
	}
}

func TestResolveActionByNameMatching(t *testing.T) {
	defs := []soar.ActionDef{
		{DisplayName: "Ping", Integration: "HTTP", ID: "10"},
		{DisplayName: "Ping", Integration: "Siemplify", ID: "11"},
		{DisplayName: "Post Data", Integration: "HTTP", ID: "12"},
	}
	if got := matchActionDefs(defs, "", "post data"); len(got) != 1 || got[0].ID.String() != "12" {
		t.Errorf("case-insensitive unique match = %+v", got)
	}
	if got := matchActionDefs(defs, "", "Ping"); len(got) != 2 {
		t.Errorf("ambiguous match = %+v", got)
	}
	if got := matchActionDefs(defs, "siemplify", "Ping"); len(got) != 1 || got[0].ID.String() != "11" {
		t.Errorf("integration-scoped match = %+v", got)
	}
	if got := matchActionDefs(defs, "", "nope"); len(got) != 0 {
		t.Errorf("no-match = %+v", got)
	}
}
