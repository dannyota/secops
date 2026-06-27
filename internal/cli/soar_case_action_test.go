package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderActionResultFaulted: a faulted action (status FAULTED / resultName
// is_failed / resultValue false / non-zero resultCode) returns a non-nil error so
// the command exits non-zero; a successful action returns nil.
func TestRenderActionResultFaulted(t *testing.T) {
	ok := []string{
		`{"resultName":"is_success","resultCode":0}`,
	}
	faulted := []string{
		`{"resultName":"is_failed","resultCode":0,"message":"boom"}`,
		`{"status":"FAULTED","resultCode":0}`,
		`{"resultValue":"false","resultCode":0}`,
		`{"resultCode":1}`,
	}
	for _, s := range ok {
		if err := renderActionResult(json.RawMessage(s)); err != nil {
			t.Errorf("success %s: want nil, got %v", s, err)
		}
	}
	for _, s := range faulted {
		if err := renderActionResult(json.RawMessage(s)); err == nil {
			t.Errorf("faulted %s: want non-nil error (non-zero exit), got nil", s)
		}
	}
}

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

// TestResolveCaseAlertGroupVerbatim: a non-empty --alert is taken verbatim as the
// alertGroupIdentifier without a live read (so the nil client is never touched).
func TestResolveCaseAlertGroupVerbatim(t *testing.T) {
	group, total, err := resolveCaseAlertGroup(t.Context(), nil, "123", "  ag-xyz  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if group != "ag-xyz" || total != 1 {
		t.Errorf("got (%q, %d), want (\"ag-xyz\", 1)", group, total)
	}
}

// TestValidateRunActionParams: a missing mandatory param is a hard error; an unknown
// key is a soft warning; LIST optionalValues are not enforced.
func TestValidateRunActionParams(t *testing.T) {
	schema := []playbookActionParam{
		{Name: "Filter Key", Type: "LIST", OptionalValues: []string{"Name", "Description"}},
		{Name: "Max Data Tables To Return", Mandatory: true},
		{Name: "Max Data Table Rows To Return", Mandatory: true},
	}

	// Both mandatory missing -> one error listing them (sorted); no warnings.
	errs, warns := validateRunActionParams(schema, map[string]string{"Filter Key": "Name"})
	if len(errs) != 1 || !strings.Contains(errs[0], "Max Data Table Rows To Return") || !strings.Contains(errs[0], "Max Data Tables To Return") {
		t.Fatalf("missing-mandatory errs = %#v", errs)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warns = %#v", warns)
	}

	// All mandatory present + a LIST value outside optionalValues (server allows it) +
	// an unknown key -> no error, one warning for the unknown key only.
	errs, warns = validateRunActionParams(schema, map[string]string{
		"Filter Key":                    "sam", // not in optionalValues — must NOT error
		"Max Data Tables To Return":     "5",
		"Max Data Table Rows To Return": "5",
		"Bogus":                         "x",
	})
	if len(errs) != 0 {
		t.Errorf("unexpected errs = %#v", errs)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "Bogus") {
		t.Errorf("warns = %#v, want one for Bogus", warns)
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
