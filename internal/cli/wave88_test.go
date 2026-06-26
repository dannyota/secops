package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

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
