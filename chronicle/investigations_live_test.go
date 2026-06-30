package chronicle_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveInvestigationTriggerRead exercises the per-alert TIN flow the web UI
// uses (confirmed against a live UI request): trigger an investigation for one
// recent alert, then read it back with the UI's filter grammar
// (alert_id='<id>' AND latest_in_alert=true, orderBy start_time desc). Gated on
// SECOPS_SIEM_SMOKE=1 — the trigger starts an AI investigation server-side.
func TestLiveInvestigationTriggerRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	end := time.Now().UTC()
	snap, err := c.GetAlerts(ctx, end.Add(-72*time.Hour), end, 1, "", "", nil)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(snap.Alerts) == 0 {
		t.Skip("no recent alert to investigate")
	}
	alertID := snap.Alerts[0].ID

	inv, err := c.TriggerInvestigation(ctx, alertID)
	if err != nil {
		t.Fatalf("trigger investigation: %v", err)
	}
	t.Logf("OK trigger -> investigation %q", inv.Name)

	filter := "alert_id='" + alertID + "' AND latest_in_alert=true"
	invs, err := c.ListInvestigationsFiltered(ctx, 100, filter, "start_time desc")
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(invs) == 0 {
		t.Errorf("filtered list returned no investigations for alert %s right after trigger", alertID)
		return
	}
	t.Logf("OK filtered list -> %d investigation(s) for the alert", len(invs))

	// The UI then reads the investigation's NOTEBOOK (the TIN working
	// document). Find the notebook reference on the investigation record and
	// fetch it.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(invs[0].Raw, &probe); err != nil {
		t.Fatalf("decode investigation: %v", err)
	}
	keys := make([]string, 0, len(probe))
	for k := range probe {
		keys = append(keys, k)
	}
	t.Logf("investigation record keys: %v", keys)
	notebookID := ""
	for k, v := range probe {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.Contains(strings.ToLower(k), "notebook") {
			notebookID = s[strings.LastIndex(s, "/")+1:]
			t.Logf("notebook ref via %q: %s", k, s)
		}
	}
	if notebookID == "" {
		t.Log("no notebook reference on the investigation record; trying the investigation id as the notebook id")
		notebookID = invs[0].Name[strings.LastIndex(invs[0].Name, "/")+1:]
	}
	nb, err := c.GetNotebook(ctx, notebookID)
	if err != nil {
		t.Logf("-- notebook read failed (id %s): %v", notebookID, err)
		return
	}
	t.Logf("OK notebook -> %d bytes", len(nb))
}

// TestLiveWave56Read exercises the Wave 56 chronicle-host reads against the
// live instance (gated on SECOPS_SIEM_SMOKE=1): the findings graph seeded from
// a recent detection, and the enrichment-agent reads (which surface a clean
// typed error where the backend is unavailable). Read-only.
func TestLiveWave56Read(t *testing.T) {
	c, ctx := liveChronicle(t)

	// Seed the graph from a recent detection of the noisiest rule.
	end := time.Now().UTC()
	start := end.Add(-72 * time.Hour)
	trends, err := c.GetRulesTrends(ctx, nil, start, end, chronicle.BucketSizeDay)
	if err != nil {
		t.Fatalf("rules trends: %v", err)
	}
	var detectionID string
	for i := range trends {
		if trends[i].TotalDetections() == 0 {
			continue
		}
		dets, err := c.ListDetections(ctx, trends[i].RuleID, start, end, "", 1)
		if err != nil || len(dets) == 0 {
			continue
		}
		detectionID = dets[0].ID
		break
	}
	if detectionID == "" {
		t.Skip("no recent detection to seed the findings graph")
	}
	raw, err := c.InitializeFindingsGraph(ctx, detectionID, start, end)
	if err != nil {
		var ae *chronicle.APIError
		if errors.As(err, &ae) {
			t.Logf("-- findingsGraph gated/unavailable: HTTP %d", ae.Status)
		} else {
			t.Errorf("findingsGraph usage bug: %v", err)
		}
	} else {
		var resp struct {
			RootNode json.RawMessage `json:"rootNode"`
			Graph    json.RawMessage `json:"graph"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Errorf("findingsGraph decode: %v", err)
		} else {
			t.Logf("OK findingsGraph root=%dB graph=%dB", len(resp.RootNode), len(resp.Graph))
		}
	}
}
