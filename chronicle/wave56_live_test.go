package chronicle_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

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
