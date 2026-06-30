package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

func TestCaseBody(t *testing.T) {
	b := caseBody(7, "")
	if b["caseId"] != 7 {
		t.Errorf("caseId = %v, want 7", b["caseId"])
	}
	if _, ok := b["alertIdentifier"]; ok {
		t.Error("empty alert must not be added to the body")
	}
	b = caseBody(7, "A1")
	if b["alertIdentifier"] != "A1" {
		t.Errorf("alertIdentifier = %v, want A1", b["alertIdentifier"])
	}
}

// runCaseDryRun executes a leaf case verb in dry-run mode (no credentials
// needed) and returns its stdout, which includes the JSON request body.
func runCaseDryRun(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmd.SetArgs(append(args, "--dry-run"))
	err := cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	if !strings.Contains(string(out), "DRY RUN") {
		t.Fatalf("expected a dry run, got:\n%s", out)
	}
	return string(out)
}

// TestCaseVerbBodies pins the swagger-verified request field names so a typo
// can't silently ship a body the live API rejects.
func TestCaseVerbBodies(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
		want []string
	}{
		{
			"assign", newCaseAssignCmd(),
			[]string{"--id", "7", "--user", "analyst1"},
			[]string{`"caseId": 7`, `"userId": "analyst1"`},
		},
		{
			"rename", newCaseRenameCmd(),
			[]string{"--id", "7", "--title", "renamed"},
			[]string{`"caseId": 7`, `"title": "renamed"`},
		},
		{
			"stage", newCaseStageCmd(),
			[]string{"--id", "7", "--stage", "Triage"},
			[]string{`"stage": "Triage"`},
		},
		{
			"tag", newCaseTagCmd(false),
			[]string{"--id", "5", "--tag", "phishing", "--alert", "A1"},
			[]string{`"tag": "phishing"`, `"alertIdentifier": "A1"`},
		},
		{
			"describe", newCaseDescribeCmd(),
			[]string{"--id", "7", "--description", "d"},
			[]string{`"description": "d"`},
		},
		{
			"importance", newCaseImportanceCmd(),
			[]string{"--id", "7", "--important=false"},
			[]string{`"isImportant": false`},
		},
		{
			"close", newCaseCloseCmd(),
			[]string{"--id", "9", "--reason", "Malicious", "--root-cause", "RC", "--comment", "done"},
			[]string{`"caseId": 9`, `"reason": "Malicious"`, `"rootCause": "RC"`, `"comment": "done"`},
		},
		{
			"merge", newCaseMergeCmd(),
			[]string{"--ids", "1,2", "--into", "3"},
			[]string{`"casesIds"`, `"caseToMergeWith": 3`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCaseDryRun(t, tc.cmd, tc.args...)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("body missing %q in:\n%s", w, out)
				}
			}
		})
	}
}

func TestExpandAlertEnum(t *testing.T) {
	cases := []struct {
		in, prefix, want string
	}{
		{"", "PRIORITY_", ""},
		{"high", "PRIORITY_", "PRIORITY_HIGH"},
		{"PRIORITY_HIGH", "PRIORITY_", "PRIORITY_HIGH"},
		{"false-positive", "", "FALSE_POSITIVE"},
		{"not-malicious", "REASON_", "REASON_NOT_MALICIOUS"},
		{"closed", "", "CLOSED"},
		{" reviewed ", "", "REVIEWED"},
	}
	for _, tc := range cases {
		if got := expandAlertEnum(tc.in, tc.prefix); got != tc.want {
			t.Errorf("expandAlertEnum(%q, %q) = %q, want %q", tc.in, tc.prefix, got, tc.want)
		}
	}
}

func TestDescribeAlertUpdate(t *testing.T) {
	sev := 80
	comment := "looks benign"
	u := chronicle.AlertUpdate{Status: "CLOSED", Verdict: "FALSE_POSITIVE", Severity: &sev, Comment: &comment}
	got := describeAlertUpdate(u)
	for _, want := range []string{"status=CLOSED", "verdict=FALSE_POSITIVE", "severity=80", "comment"} {
		if !strings.Contains(got, want) {
			t.Errorf("describeAlertUpdate missing %q in %q", want, got)
		}
	}
}

func TestAlertUpdateValidate(t *testing.T) {
	if err := (chronicle.AlertUpdate{}).Validate(); err == nil {
		t.Error("empty update must not validate")
	}
	if err := (chronicle.AlertUpdate{Status: "BOGUS"}).Validate(); err == nil {
		t.Error("invalid status must not validate")
	}
	if err := (chronicle.AlertUpdate{Status: "CLOSED", Verdict: "FALSE_POSITIVE"}).Validate(); err != nil {
		t.Errorf("valid update rejected: %v", err)
	}
}

func TestSOARIDRows(t *testing.T) {
	mk := func(body string) chronicle.LegacyCase {
		var c chronicle.LegacyCase
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return c
	}
	uuids := []string{"AAAA-1111", "BBBB-2222"}
	// Out-of-order response with body ids: pairing must follow the body id.
	rows := soarIDRows(uuids, []chronicle.LegacyCase{
		mk(`{"id":"BBBB-2222","soarPlatformInfo":{"caseId":"42"}}`),
		mk(`{"id":"AAAA-1111","soarPlatformInfo":{"caseId":"7"}}`),
	})
	if rows[0].SOARCaseID != "7" || rows[1].SOARCaseID != "42" {
		t.Errorf("id-keyed pairing failed: %+v", rows)
	}
	// No body id: positional fallback.
	rows = soarIDRows(uuids, []chronicle.LegacyCase{
		mk(`{"soarPlatformInfo":{"caseId":"7"}}`),
		mk(`{"soarPlatformInfo":{"caseId":"42"}}`),
	})
	if rows[0].SOARCaseID != "7" || rows[1].SOARCaseID != "42" {
		t.Errorf("positional pairing failed: %+v", rows)
	}
	// Missing linkage renders empty (the CLI prints a dash).
	rows = soarIDRows([]string{"CCCC"}, []chronicle.LegacyCase{mk(`{"id":"CCCC"}`)})
	if rows[0].SOARCaseID != "" {
		t.Errorf("missing linkage should stay empty: %+v", rows)
	}
}

func TestParseCasePriority(t *testing.T) {
	good := map[string]legacy.CasePriority{
		"low": legacy.PriorityLow, "MEDIUM": legacy.PriorityMedium, "high": legacy.PriorityHigh,
		"critical": legacy.PriorityCritical, "informative": legacy.PriorityInformative,
		"info": legacy.PriorityInformative, "40": legacy.PriorityLow,
	}
	for in, want := range good {
		got, err := legacy.ParseCasePriority(in)
		if err != nil || got != want {
			t.Errorf("ParseCasePriority(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	// Only the server's defined codings are accepted: an arbitrary int would
	// otherwise reach the wire as an undefined priority, and 0 ("Unchanged")
	// would be a silent no-op.
	for _, bad := range []string{"urgent", "50", "0", "7"} {
		if _, err := legacy.ParseCasePriority(bad); err == nil {
			t.Errorf("ParseCasePriority(%q) must error", bad)
		}
	}
}

func TestParseCloseReasonIntRange(t *testing.T) {
	if r, err := parseCloseReason("0"); err != nil || r != legacy.CloseMalicious {
		t.Errorf("parseCloseReason(0) = %v, %v; want Malicious", r, err)
	}
	// An out-of-range int must be rejected, not stringified as "CloseReason(N)".
	for _, bad := range []string{"7", "-1", "99"} {
		if _, err := parseCloseReason(bad); err == nil {
			t.Errorf("parseCloseReason(%q) must error", bad)
		}
	}
}

func TestParseAlertCloseReason(t *testing.T) {
	for in, want := range map[string]string{
		"malicious": "Malicious", "not-malicious": "NotMalicious",
		"maintenance": "Maintenance", "inconclusive": "Inconclusive",
	} {
		got, err := parseAlertCloseReason(in)
		if err != nil || got != want {
			t.Errorf("parseAlertCloseReason(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// Alerts take no Unknown — the case-close vocabulary's extra value is rejected.
	if _, err := parseAlertCloseReason("unknown"); err == nil {
		t.Error("alert close must reject 'unknown'")
	}
}

func TestParseAlertUsefulness(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "none": "None", "useful": "Useful", "not-useful": "NotUseful",
	} {
		got, err := parseAlertUsefulness(in)
		if err != nil || got != want {
			t.Errorf("parseAlertUsefulness(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseAlertUsefulness("very"); err == nil {
		t.Error("invalid usefulness must error")
	}
}

func TestParseSince(t *testing.T) {
	if ts, err := parseSince(""); err != nil || !ts.IsZero() {
		t.Errorf("empty since = %v, %v; want zero", ts, err)
	}
	ts, err := parseSince("24h")
	if err != nil {
		t.Fatalf("duration since: %v", err)
	}
	if d := time.Since(ts); d < 23*time.Hour || d > 25*time.Hour {
		t.Errorf("24h since off: %v ago", d)
	}
	if ts, err = parseSince("2026-01-02"); err != nil || ts.Format("2006-01-02") != "2026-01-02" {
		t.Errorf("date since = %v, %v", ts, err)
	}
	if _, err = parseSince("yesterday"); err == nil {
		t.Error("invalid since must error")
	}
}

func TestMatchModernCase(t *testing.T) {
	mk := func(body string) soar.Case {
		var c soar.Case
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return c
	}
	cs := mk(`{"displayId":"9","displayName":"Phish","priority":"PRIORITY_HIGH","status":"OPENED",
	  "assignee":"analyst@example.com","tags":[{"displayName":"ransomware"},"phishing"],
	  "updateTime":"2026-06-01T10:00:00Z"}`)

	if !matchModernCase(&cs, caseListFilters{}) {
		t.Error("no filters must match")
	}
	if !matchModernCase(&cs, caseListFilters{assignee: "ANALYST"}) {
		t.Error("assignee substring (case-insensitive) must match")
	}
	if matchModernCase(&cs, caseListFilters{assignee: "other"}) {
		t.Error("wrong assignee must not match")
	}
	if !matchModernCase(&cs, caseListFilters{priority: legacy.PriorityHigh}) {
		t.Error("priority High must match PRIORITY_HIGH")
	}
	if matchModernCase(&cs, caseListFilters{priority: legacy.PriorityLow}) {
		t.Error("priority Low must not match PRIORITY_HIGH")
	}
	// The informative level maps to the modern PRIORITY_INFO token.
	info := mk(`{"priority":"PRIORITY_INFO","status":"OPENED"}`)
	if !matchModernCase(&info, caseListFilters{priority: legacy.PriorityInformative}) {
		t.Error("priority Informative must match PRIORITY_INFO")
	}
	// Tags: both the object and bare-string element shapes.
	if !matchModernCase(&cs, caseListFilters{tag: "Ransomware"}) {
		t.Error("object tag must match case-insensitively")
	}
	if !matchModernCase(&cs, caseListFilters{tag: "phishing"}) {
		t.Error("string tag must match")
	}
	if matchModernCase(&cs, caseListFilters{tag: "malware"}) {
		t.Error("absent tag must not match")
	}
	cut, _ := time.Parse(time.RFC3339, "2026-05-01T00:00:00Z")
	if !matchModernCase(&cs, caseListFilters{since: cut}) {
		t.Error("updateTime after cutoff must match")
	}
	cut, _ = time.Parse(time.RFC3339, "2026-06-02T00:00:00Z")
	if matchModernCase(&cs, caseListFilters{since: cut}) {
		t.Error("updateTime before cutoff must not match")
	}
}

func TestMatchLegacyCard(t *testing.T) {
	card := soarCaseCard{
		ID: 5, Title: "x", Priority: 80, Status: 1,
		AssignedUserName: "analyst1", CreationTimeMs: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
	if !matchLegacyCard(&card, caseListFilters{}) {
		t.Error("zero filters must match")
	}
	if !matchLegacyCard(&card, caseListFilters{assignee: "analyst", priority: legacy.PriorityHigh}) {
		t.Error("matching filters must match")
	}
	if matchLegacyCard(&card, caseListFilters{priority: legacy.PriorityLow}) {
		t.Error("wrong priority must not match")
	}
	cut := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	if matchLegacyCard(&card, caseListFilters{since: cut}) {
		t.Error("created before cutoff must not match")
	}
}

func TestEmitSOARCaseFullRuleGenerator(t *testing.T) {
	raw := json.RawMessage(`{"id": 9, "title": "t", "alerts": [
	  {"identifier": "A-1", "name": "Brute force", "priority": 80,
	   "additionalProperties": {"ruleGenerator": "Suspicious Login Burst",
	     "rule_id": "ru_00000000-0000-0000-0000-000000000000"}}
	]}`)
	var buf bytes.Buffer
	if err := emitSOARCaseFull(&buf, raw); err != nil {
		t.Fatalf("emitSOARCaseFull: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "rule: Suspicious Login Burst") ||
		!strings.Contains(out, "(ru_00000000-0000-0000-0000-000000000000)") ||
		!strings.Contains(out, `rules detections "Suspicious Login Burst"`) {
		t.Errorf("missing rule pivot in:\n%s", out)
	}
}

func TestFilterLegacyCardsJSONShape(t *testing.T) {
	raw := json.RawMessage(`{"caseCards":[
	  {"id":1,"assignedUserName":"a","priority":80,"extraField":"kept"},
	  {"id":2,"assignedUserName":"b","priority":40}
	],"totalCount":2}`)
	page, err := filterLegacyCards(raw, caseListFilters{priority: legacy.PriorityHigh})
	if err != nil {
		t.Fatalf("filterLegacyCards: %v", err)
	}
	if len(page.Typed) != 1 || page.Typed[0].ID != 1 {
		t.Errorf("filter kept %+v, want only id 1", page.Typed)
	}
	// The raw view stays lossless and the envelope shape is preserved under --json.
	var buf bytes.Buffer
	if err := emitSOARCaseCardsJSON(&buf, raw, caseListFilters{priority: legacy.PriorityHigh}); err != nil {
		t.Fatalf("emitSOARCaseCardsJSON: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"caseCards"`, `"totalCount"`, `"extraField"`} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered --json missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"id": 2`) {
		t.Errorf("filtered --json leaked the excluded card:\n%s", out)
	}
}

func TestSOARIDRowsPositionalGating(t *testing.T) {
	mk := func(body string) chronicle.LegacyCase {
		var c chronicle.LegacyCase
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return c
	}
	// A response that is not a clean 1:1 echo (fewer cases than uuids, no body
	// ids) must NOT pair positionally — a wrong pairing would feed a mutating
	// verb the wrong production case id.
	rows := soarIDRows([]string{"AAAA", "BBBB"}, []chronicle.LegacyCase{
		mk(`{"soarPlatformInfo":{"caseId":"42"}}`),
	})
	if rows[0].SOARCaseID != "" || rows[1].SOARCaseID != "" {
		t.Errorf("partial unkeyed response must not pair positionally: %+v", rows)
	}
	// Mixed keyed + unkeyed: the keyed pairing wins and the unkeyed case is not
	// positionally attributed to a different uuid.
	rows = soarIDRows([]string{"AAAA", "BBBB"}, []chronicle.LegacyCase{
		mk(`{"soarPlatformInfo":{"caseId":"42"}}`),
		mk(`{"id":"AAAA","soarPlatformInfo":{"caseId":"7"}}`),
	})
	if rows[0].SOARCaseID != "7" || rows[1].SOARCaseID != "" {
		t.Errorf("mixed response mispaired: %+v", rows)
	}
}
