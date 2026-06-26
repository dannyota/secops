package cli

import "testing"

// TestBuildManualActionBody locks the ExecuteManualAction request shape:
// actionProvider is always "Scripts", caseId is a STRING, and the action name is
// qualified <integration>_<action>. A bare action name (e.g. "Ping") is rejected by
// the server for a marketplace integration's action, so it must be qualified.
func TestBuildManualActionBody(t *testing.T) {
	body := buildManualActionBody(123, "GoogleChronicle", "Ping", "All entities", "inst-uuid", "{}", "ag-1")
	if body["actionProvider"] != "Scripts" {
		t.Errorf("actionProvider = %v, want Scripts (always)", body["actionProvider"])
	}
	if body["caseId"] != "123" {
		t.Errorf("caseId = %#v, want string \"123\"", body["caseId"])
	}
	if body["actionName"] != "GoogleChronicle_Ping" {
		t.Errorf("actionName = %v, want GoogleChronicle_Ping (qualified)", body["actionName"])
	}
	props := body["properties"].(map[string]string)
	if props["ScriptName"] != "GoogleChronicle_Ping" || props["IntegrationInstance"] != "inst-uuid" {
		t.Errorf("properties wrong: %#v", props)
	}
	if got := body["alertGroupIdentifiers"].([]string); len(got) != 1 || got[0] != "ag-1" {
		t.Errorf("alertGroupIdentifiers = %#v", got)
	}

	// No --integration: a built-in Scripts action keeps its already-qualified name,
	// and an empty alert omits the alert-group key.
	body = buildManualActionBody(1, "", "HTTP_Ping", "All entities", "u", "{}", "")
	if body["actionName"] != "HTTP_Ping" {
		t.Errorf("bare Scripts action = %v, want HTTP_Ping unchanged", body["actionName"])
	}
	if _, present := body["alertGroupIdentifiers"]; present {
		t.Errorf("alertGroupIdentifiers must be omitted when alert is empty")
	}
}

// TestQualifyActionName: integration prefixes the action; an already-qualified name is
// never double-prefixed; empty integration is a no-op.
func TestQualifyActionName(t *testing.T) {
	cases := []struct{ integ, action, want string }{
		{"GoogleChronicle", "Ping", "GoogleChronicle_Ping"},
		{"GoogleChronicle", "GoogleChronicle_Ping", "GoogleChronicle_Ping"}, // no double prefix
		{"", "HTTP_Ping", "HTTP_Ping"},
		{"  GoogleChronicle  ", " Get Data Tables ", "GoogleChronicle_Get Data Tables"},
	}
	for _, c := range cases {
		if got := qualifyActionName(c.integ, c.action); got != c.want {
			t.Errorf("qualifyActionName(%q,%q) = %q, want %q", c.integ, c.action, got, c.want)
		}
	}
}
