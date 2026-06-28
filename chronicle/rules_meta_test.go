package chronicle

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestRuleFullViewUnmarshal pins that the enriched Rule fields (FULL view) and
// the MITRE meta accessors decode from a representative rule payload.
func TestRuleFullViewUnmarshal(t *testing.T) {
	const payload = `{
		"name": "projects/p/locations/r/instances/i/rules/ru_1111aaaa-2222-3333-4444-555566667777",
		"displayName": "demo_rule",
		"type": "MULTI_EVENT",
		"author": "analyst",
		"compilationState": "SUCCEEDED",
		"runFrequency": "HOURLY",
		"liveModeEnabled": true,
		"alertingEnabled": true,
		"revisionId": "v_1700000000_000000000",
		"inputsUsed": {"usesUdm": true, "usesEntity": false},
		"metadata": {
			"mitre_tactic": "TA0005, TA0002",
			"mitre_technique": "T1059.001, T1003",
			"mitre_technique_name": "Impair Defenses",
			"priority": "HIGH_PRIORITY"
		}
	}`
	var r Rule
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Author != "analyst" || r.CompilationState != "SUCCEEDED" || r.RunFrequency != "HOURLY" {
		t.Errorf("scalar fields wrong: %+v", r)
	}
	if !r.LiveModeEnabled || !r.AlertingEnabled {
		t.Errorf("deployment-state fields not decoded: %+v", r)
	}
	if !r.InputsUsed["usesUdm"] || r.InputsUsed["usesEntity"] {
		t.Errorf("inputsUsed wrong: %v", r.InputsUsed)
	}
	if r.Metadata["priority"] != "HIGH_PRIORITY" {
		t.Errorf("metadata map wrong: %v", r.Metadata)
	}
	// Tactics/techniques split on comma AND whitespace, sorted + de-duped.
	if got, want := r.MitreTactics(), []string{"TA0002", "TA0005"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MitreTactics() = %v, want %v", got, want)
	}
	// Gathers from both mitre_technique (comma-list of ids) and mitre_technique_name;
	// the multi-word name "Impair Defenses" must stay INTACT (not split on the space).
	if got, want := r.MitreTechniques(), []string{"Impair Defenses", "T1003", "T1059.001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MitreTechniques() = %v, want %v", got, want)
	}
}

// TestRuleMitreEmpty confirms a rule with no MITRE meta yields no tactics/techniques.
func TestRuleMitreEmpty(t *testing.T) {
	r := Rule{DisplayName: "x"}
	if got := r.MitreTactics(); got != nil {
		t.Errorf("MitreTactics() on bare rule = %v, want nil", got)
	}
	if got := r.MitreTechniques(); got != nil {
		t.Errorf("MitreTechniques() on bare rule = %v, want nil", got)
	}
}
