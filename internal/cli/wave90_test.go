package cli

import (
	"encoding/json"
	"testing"

	"danny.vn/secops/chronicle"
)

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
