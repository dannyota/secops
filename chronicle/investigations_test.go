package chronicle

import (
	"encoding/json"
	"testing"
)

// The two investigation lifecycle shapes, as the API returns them: in progress
// (status only) and completed (verdict/confidence/summary/nextSteps). Pinning
// the decode keeps the typed view honest against the live wire shape.
func TestInvestigationDecodeLifecycle(t *testing.T) {
	inProgress := `{
		"name": "projects/p/locations/l/instances/i/investigations/abc-123",
		"status": "STATUS_IN_PROGRESS",
		"triggerType": "AGENT_MANUAL",
		"notebook": "projects/p/locations/l/instances/i/notebooks/nb-456",
		"alerts": {"ids": ["de_00000000-0000-0000-0000-000000000000"]}
	}`
	var inv Investigation
	if err := json.Unmarshal([]byte(inProgress), &inv); err != nil {
		t.Fatalf("decode in-progress: %v", err)
	}
	if inv.Completed() {
		t.Error("STATUS_IN_PROGRESS must not be Completed()")
	}
	if inv.InvestigationID() != "abc-123" {
		t.Errorf("InvestigationID = %q", inv.InvestigationID())
	}
	if inv.NotebookID() != "nb-456" {
		t.Errorf("NotebookID = %q", inv.NotebookID())
	}
	if inv.TriggerType != "AGENT_MANUAL" {
		t.Errorf("TriggerType = %q", inv.TriggerType)
	}
	if len(inv.Raw) == 0 {
		t.Error("Raw must retain the full object")
	}

	completed := `{
		"name": "projects/p/locations/l/instances/i/investigations/abc-123",
		"status": "STATUS_COMPLETED_SUCCESS",
		"verdict": "FALSE_POSITIVE",
		"confidence": "HIGH_CONFIDENCE",
		"summary": "## Finding\nBenign.",
		"nextSteps": [
			{"title": "Search for related logins", "type": "SEARCHABLE"},
			{"title": "Confirm with the asset owner", "type": "MANUAL"}
		],
		"publishTime": "2026-01-01T00:00:00Z"
	}`
	inv = Investigation{}
	if err := json.Unmarshal([]byte(completed), &inv); err != nil {
		t.Fatalf("decode completed: %v", err)
	}
	if !inv.Completed() {
		t.Error("STATUS_COMPLETED_SUCCESS must be Completed()")
	}
	if inv.Verdict != "FALSE_POSITIVE" || inv.Confidence != "HIGH_CONFIDENCE" {
		t.Errorf("verdict fields = %q/%q", inv.Verdict, inv.Confidence)
	}
	if len(inv.NextSteps) != 2 || inv.NextSteps[1].Type != "MANUAL" {
		t.Errorf("nextSteps = %+v", inv.NextSteps)
	}
	if inv.NotebookID() != "" {
		t.Errorf("NotebookID on a record without notebook = %q", inv.NotebookID())
	}

	// Completed() must also hold for non-success terminal states.
	inv = Investigation{Status: "STATUS_COMPLETED_FAILURE"}
	if !inv.Completed() {
		t.Error("STATUS_COMPLETED_FAILURE must be Completed()")
	}
	var nilInv *Investigation
	if nilInv.Completed() || nilInv.NotebookID() != "" || nilInv.InvestigationID() != "" {
		t.Error("nil receiver helpers must be safe")
	}
}
