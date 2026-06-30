package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestResolveAlertIDsArgsDedup asserts positional ids are de-duplicated and
// order-preserved, and that whitespace-only entries are dropped. (The --where /
// --stdin-ids paths require a live client / stdin and are covered live.)
func TestResolveAlertIDsArgsDedup(t *testing.T) {
	ids, err := resolveAlertIDs([]string{"de_1", " de_2 ", "de_1", "", "de_3"}, "", false, 24, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"de_1", "de_2", "de_3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("resolveAlertIDs = %v, want %v", ids, want)
	}
}

// `alerts investigate` is registered, takes exactly one positional alert id,
// and refuses to start a generation in read-only mode BEFORE touching any
// client/credentials — while --latest stays available as the read-only path.
func TestAlertsInvestigateCommand(t *testing.T) {
	root := newAlertsCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "investigate" {
			found = true
			if c.Flags().Lookup("latest") == nil {
				t.Error("investigate must have --latest")
			}
			if err := c.Args(c, []string{}); err == nil {
				t.Error("investigate must require an alert id")
			}
			if err := c.Args(c, []string{"a", "b"}); err == nil {
				t.Error("investigate must reject extra args")
			}
		}
	}
	if !found {
		t.Fatal("alerts investigate not registered")
	}
}

func TestAlertsInvestigateReadOnlyRefusal(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "1")
	root := newAlertsCmd()
	root.SetArgs([]string{"investigate", "de_00000000-0000-0000-0000-000000000000"})
	root.SilenceUsage, root.SilenceErrors = true, true
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("read-only mode must refuse the generation before any API call, got %v", err)
	}
}

// TestCollectAlertEntities verifies the enrichment view pulls hosts, users,
// process files (path + sha256), and about[] urls out of a collection's mapped
// UDM events — deduped, first-seen order preserved.
func TestCollectAlertEntities(t *testing.T) {
	elements := []json.RawMessage{json.RawMessage(`{
	  "references": [
	    {"event": {
	      "principal": {"hostname": "HOST1", "user": {"userid": "DOM\\svc"},
	        "process": {"file": {"fullPath": "C:\\a\\powershell.exe", "sha256": "aaa"}}},
	      "target": {"process": {"file": {"fullPath": "C:\\b\\conhost.exe", "sha256": "bbb"}}},
	      "about": [{"url": "evil.example.net"}]
	    }},
	    {"event": {"principal": {"hostname": "HOST1"}}}
	  ]
	}`)}

	ents := collectAlertEntities(elements)
	want := []alertEntity{
		{"host", "HOST1"},
		{"user", "DOM\\svc"},
		{"process", "C:\\a\\powershell.exe  aaa"},
		{"process", "C:\\b\\conhost.exe  bbb"},
		{"url", "evil.example.net"},
	}
	if len(ents) != len(want) {
		t.Fatalf("got %d entities, want %d: %+v", len(ents), len(want), ents)
	}
	for i, w := range want {
		if ents[i] != w {
			t.Errorf("entity[%d] = %+v, want %+v", i, ents[i], w)
		}
	}
}

// TestLegacyCollectionUnmarshal confirms the typed summary fields decode while
// the full object is retained in Raw.
func TestLegacyCollectionUnmarshal(t *testing.T) {
	var col chronicle.LegacyCollection
	body := `{"id":"de_x","caseName":"uuid-1","tags":["TA0002"],
	  "detection":[{"ruleName":"R","severity":"LOW","ruleSetDisplayName":"RS"}],
	  "feedbackSummary":{"status":"OPEN","priorityDisplay":"Low","triageAgentInvestigationId":"inv-1"}}`
	if err := json.Unmarshal([]byte(body), &col); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if col.ID != "de_x" || col.CaseName != "uuid-1" {
		t.Errorf("id/case = %q/%q", col.ID, col.CaseName)
	}
	if len(col.Detection) != 1 || col.Detection[0].RuleName != "R" {
		t.Errorf("detection = %+v", col.Detection)
	}
	if col.FeedbackSummary == nil || col.FeedbackSummary.TriageAgentInvestigationID != "inv-1" {
		t.Errorf("feedbackSummary = %+v", col.FeedbackSummary)
	}
	if len(col.Raw) == 0 {
		t.Error("Raw should retain the full object")
	}
}
