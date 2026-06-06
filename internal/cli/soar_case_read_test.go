package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/soar/legacy"
)

func TestParseSOARCaseStatuses(t *testing.T) {
	cases := []struct {
		in   string
		want []int
		err  bool
	}{
		{"", []int{legacy.CaseStatusOpen}, false},
		{"open", []int{legacy.CaseStatusOpen}, false},
		{"OPEN", []int{legacy.CaseStatusOpen}, false},
		{"closed", []int{legacy.CaseStatusClosed}, false},
		{"all", []int{legacy.CaseStatusOpen, legacy.CaseStatusClosed}, false},
		{"bogus", nil, true},
	}
	for _, tc := range cases {
		got, err := parseSOARCaseStatuses(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("parseSOARCaseStatuses(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSOARCaseStatuses(%q): unexpected error %v", tc.in, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseSOARCaseStatuses(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseSOARCaseStatuses(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestSOARPriorityName(t *testing.T) {
	cases := map[int]string{
		-1:  "Informative",
		40:  "Low",
		60:  "Medium",
		80:  "High",
		100: "Critical",
		7:   "7", // unmapped falls back to the raw number
	}
	for in, want := range cases {
		if got := soarPriorityName(in); got != want {
			t.Errorf("soarPriorityName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSOARCaseStatusName(t *testing.T) {
	if got := soarCaseStatusName(legacy.CaseStatusOpen); got != "OPEN" {
		t.Errorf("status open = %q, want OPEN", got)
	}
	if got := soarCaseStatusName(legacy.CaseStatusClosed); got != "CLOSED" {
		t.Errorf("status closed = %q, want CLOSED", got)
	}
	if got := soarCaseStatusName(5); got != "5" {
		t.Errorf("unmapped status = %q, want 5", got)
	}
}

func TestMsToUTC(t *testing.T) {
	if got := msToUTC(0); got != "-" {
		t.Errorf("msToUTC(0) = %q, want -", got)
	}
	// 2021-01-01T00:00:00Z == 1609459200000 ms
	if got := msToUTC(1609459200000); got != "2021-01-01 00:00" {
		t.Errorf("msToUTC = %q, want 2021-01-01 00:00", got)
	}
}

func TestEmitSOARCaseCards(t *testing.T) {
	raw := json.RawMessage(`{
	  "caseCards": [
	    {"id": 101, "title": "Suspicious login", "priority": 80, "status": 1,
	     "stage": "Triage", "assignedUserName": "analyst1", "alertsCount": 3},
	    {"id": 102, "title": "Port scan", "priority": 40, "status": 2,
	     "stage": "Investigation", "assignedUserName": "", "alertsCount": 1}
	  ],
	  "totalCount": 7
	}`)
	var buf bytes.Buffer
	if err := emitSOARCaseCards(&buf, raw, 0); err != nil {
		t.Fatalf("emitSOARCaseCards: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"101", "Suspicious login", "High", "OPEN", "Triage", "analyst1",
		"102", "Port scan", "Low", "CLOSED", "(of 7 total)"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in:\n%s", want, out)
		}
	}
}

func TestEmitSOARCaseCardsLimit(t *testing.T) {
	raw := json.RawMessage(`{"caseCards":[
	  {"id":1,"title":"alpha"},{"id":2,"title":"beta"},{"id":3,"title":"gamma"}
	],"totalCount":3}`)
	var buf bytes.Buffer
	if err := emitSOARCaseCards(&buf, raw, 1); err != nil {
		t.Fatalf("emitSOARCaseCards: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "1 case(s)") || !strings.Contains(out, "alpha") {
		t.Errorf("limit not applied, got:\n%s", out)
	}
	if strings.Contains(out, "beta") || strings.Contains(out, "gamma") {
		t.Errorf("limit leaked extra rows:\n%s", out)
	}
}

func TestEmitSOARCaseCardsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := emitSOARCaseCards(&buf, json.RawMessage(`{"caseCards":[],"totalCount":0}`), 0); err != nil {
		t.Fatalf("emitSOARCaseCards: %v", err)
	}
	if !strings.Contains(buf.String(), "no cases.") {
		t.Errorf("want 'no cases.', got:\n%s", buf.String())
	}
}

func TestEmitSOARCaseFull(t *testing.T) {
	raw := json.RawMessage(`{
	  "id": 555, "title": "Credential theft", "priority": 100, "status": 1,
	  "stage": "Triage", "assignedUserName": "analyst2", "environment": "Default Environment",
	  "isImportant": true, "isIncident": false, "description": "exfil attempt",
	  "alerts": [
	    {"id": 9001, "identifier": "ALERT-A", "name": "Brute force", "product": "Auth", "priority": 80},
	    {"id": 9002, "identifier": "ALERT-B", "name": "Impossible travel", "product": "IdP", "priority": 60}
	  ]
	}`)
	var buf bytes.Buffer
	if err := emitSOARCaseFull(&buf, raw); err != nil {
		t.Fatalf("emitSOARCaseFull: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Case 555", "Credential theft", "Critical", "OPEN", "Triage",
		"analyst2", "Default Environment", "exfil attempt", "Alerts (2)",
		"9001", "Brute force", "High", "9002", "Impossible travel"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view missing %q in:\n%s", want, out)
		}
	}
}

func TestEmitSOARCaseFullNoAlerts(t *testing.T) {
	var buf bytes.Buffer
	if err := emitSOARCaseFull(&buf, json.RawMessage(`{"id":1,"title":"t","alerts":[]}`)); err != nil {
		t.Fatalf("emitSOARCaseFull: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Alerts (0)") || !strings.Contains(out, "none.") {
		t.Errorf("want zero-alert rendering, got:\n%s", out)
	}
}
