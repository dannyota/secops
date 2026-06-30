package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/soar"
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
	if err := emitSOARCaseCards(&buf, raw, 0, caseListFilters{}); err != nil {
		t.Fatalf("emitSOARCaseCards: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"101", "Suspicious login", "High", "OPEN", "Triage", "analyst1",
		"102", "Port scan", "Low", "CLOSED", "(of 7 total)",
	} {
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
	if err := emitSOARCaseCards(&buf, raw, 1, caseListFilters{}); err != nil {
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
	if err := emitSOARCaseCards(&buf, json.RawMessage(`{"caseCards":[],"totalCount":0}`), 0, caseListFilters{}); err != nil {
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
	    {"identifier": "ALERT-A", "name": "Brute force", "product": "Auth", "priority": 80},
	    {"identifier": "ALERT-B", "name": "Impossible travel", "product": "IdP", "priority": 60}
	  ]
	}`)
	var buf bytes.Buffer
	if err := emitSOARCaseFull(&buf, raw); err != nil {
		t.Fatalf("emitSOARCaseFull: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Case 555", "Credential theft", "Critical", "OPEN", "Triage",
		"analyst2", "Default Environment", "exfil attempt", "Alerts (2)",
		"--alert ALERT-A", "Brute force", "High", "--alert ALERT-B", "Impossible travel",
	} {
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

// TestEmitCaseWallTimeline verifies the wall renders as a chronological timeline
// (oldest first) with each record's activity comment surfaced — not a bare count.
func TestEmitCaseWallTimeline(t *testing.T) {
	raw := json.RawMessage(`{"caseWallRecords":[
	  {"activityKind":"ACTION","createTime":2000,"activityDataJson":"{\"comment\":\"Alerts grouped to the case\"}"},
	  {"activityKind":"CASE_STAGE_CHANGED","createTime":1000,"activityDataJson":"{\"comment\":\"Case stage set to Triage\"}"},
	  {"activityKind":"PLAYBOOK_ATTACHED","createTime":1500,"activityDataJson":"{\"comment\":\"Playbook Notify Case to Teams attached\"}"}
	],"totalSize":3}`)
	var sb strings.Builder
	if err := emitCaseWall(&sb, raw); err != nil {
		t.Fatalf("emitCaseWall: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"Case stage set to Triage", "Playbook Notify Case to Teams attached", "Alerts grouped to the case", "3 record(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("wall output missing %q\n%s", want, out)
		}
	}
	// Oldest first: the stage change (t=1000) must precede the grouping (t=2000).
	if strings.Index(out, "Case stage set") > strings.Index(out, "Alerts grouped") {
		t.Errorf("wall not sorted oldest-first:\n%s", out)
	}
}

// TestWallActivityFallback checks the comment is preferred, with activityDescription
// as a fallback, and "None"/empty are ignored.
func TestWallActivityFallback(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"comment":"did a thing"}`, "did a thing"},
		{`{"comment":"None","activityDescription":"desc"}`, "desc"},
		{`{"comment":"","activityDescription":""}`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := wallActivity(c.in); got != c.want {
			t.Errorf("wallActivity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestWallEpoch handles both the unix-millis number and an RFC3339 string form.
func TestWallEpoch(t *testing.T) {
	if got := wallEpoch(json.RawMessage(`1750000000000`)); got != 1750000000000 {
		t.Errorf("numeric epoch = %d", got)
	}
	if got := wallEpoch(json.RawMessage(`"2026-06-25T10:00:00Z"`)); got <= 0 {
		t.Errorf("RFC3339 epoch should be positive, got %d", got)
	}
	if got := wallEpoch(json.RawMessage(`""`)); got != 0 {
		t.Errorf("empty epoch = %d, want 0", got)
	}
}

// TestSummaryStepTrace verifies --steps renders the per-step execution trace
// (every completed step), not just counts — so a run that finished but did the
// wrong thing is debuggable.
func TestSummaryStepTrace(t *testing.T) {
	raw := json.RawMessage(`{"completedSteps":[
	  {"status":"COMPLETED","integration":"HTTP","actionName":"HTTP_Post Data","message":"Response data: ok","creationTimeUnixTimeInMs":2000},
	  {"status":"COMPLETED","integration":"Tools","actionName":"Ping","message":"reachable","creationTimeUnixTimeInMs":1000}
	],"faultedSteps":[]}`)

	var with, without strings.Builder
	printWorkflowSummary(&with, raw, false, true)     // --steps
	printWorkflowSummary(&without, raw, false, false) // default
	w, wo := with.String(), without.String()

	if !strings.Contains(w, "execution trace") || !strings.Contains(w, "HTTP_Post Data") || !strings.Contains(w, "Ping") {
		t.Errorf("--steps should render the step trace:\n%s", w)
	}
	// Oldest first: Ping (t=1000) before HTTP_Post Data (t=2000).
	if strings.Index(w, "Ping") > strings.Index(w, "HTTP_Post Data") {
		t.Errorf("step trace not oldest-first:\n%s", w)
	}
	// Default (no --steps) shows counts only, not the per-step action names.
	if strings.Contains(wo, "HTTP_Post Data") {
		t.Errorf("default summary should not dump steps:\n%s", wo)
	}
}

// TestEmitCaseWallEmpty renders a clear message rather than an empty table.
func TestEmitCaseWallEmpty(t *testing.T) {
	var sb strings.Builder
	if err := emitCaseWall(&sb, json.RawMessage(`{"caseWallRecords":[]}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "no case wall records") {
		t.Errorf("empty wall should say so, got %q", sb.String())
	}
}

func TestCaseSummarySettled(t *testing.T) {
	var s soar.CaseSummary
	if err := json.Unmarshal([]byte(`{"summary":"x","state":"IN_PROGRESS"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Settled() {
		t.Error("IN_PROGRESS must not be settled")
	}
	if err := json.Unmarshal([]byte(`{"summary":"x","state":"SUCCESSFUL","reasons":["r"]}`), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Settled() || len(s.Reasons) != 1 {
		t.Errorf("settled decode = %+v", s)
	}
}

func TestCaseAlertDecode(t *testing.T) {
	var a soar.CaseAlert
	body := `{"id": 24506, "identifier": "ALERT-X", "alertGroupIdentifier": "GRP-1", "displayName": "Alert"}`
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatal(err)
	}
	if a.ID.String() != "24506" || a.Identifier != "ALERT-X" {
		t.Errorf("decode = %+v", a)
	}
}
