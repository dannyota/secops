package chronicle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTrendsQueryAlignsBuckets(t *testing.T) {
	start := time.Date(2026, 6, 3, 7, 45, 11, 0, time.UTC)
	end := time.Date(2026, 6, 10, 8, 1, 0, 0, time.UTC)
	q := trendsQuery([]string{"ru_a", " ", "ru_b"}, start, end, BucketSizeDay)

	// Day buckets: floor the start, ceil the end (unaligned boundaries 400).
	if got := q.Get("bucketTimeRange.startTime"); got != "2026-06-03T00:00:00Z" {
		t.Errorf("start = %s, want floored to midnight", got)
	}
	if got := q.Get("bucketTimeRange.endTime"); got != "2026-06-11T00:00:00Z" {
		t.Errorf("end = %s, want ceiled to next midnight", got)
	}
	if got := q.Get("bucketSize"); got != BucketSizeDay {
		t.Errorf("bucketSize = %s", got)
	}
	if ids := q["ruleId"]; len(ids) != 2 || ids[0] != "ru_a" || ids[1] != "ru_b" {
		t.Errorf("ruleId params = %v (blank entries must drop)", ids)
	}

	// Already-aligned end stays put (no extra day appended).
	aligned := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	q = trendsQuery(nil, start, aligned, "")
	if got := q.Get("bucketTimeRange.endTime"); got != "2026-06-11T00:00:00Z" {
		t.Errorf("aligned end moved to %s", got)
	}
}

func TestRuleTrendTotalDetections(t *testing.T) {
	var tr RuleTrend
	body := `{"ruleId":"ru_x","lastDetectionTime":"2026-06-09T17:18:00Z","detectionCounts":[
	  {"timeRange":{"startTime":"a","endTime":"b"},"detectionCount":3,"scannedStatus":"PARTIAL"},
	  {"timeRange":{"startTime":"b","endTime":"c"},"scannedStatus":"FULL"},
	  {"timeRange":{"startTime":"c","endTime":"d"},"detectionCount":7}
	]}`
	if err := json.Unmarshal([]byte(body), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.RuleID != "ru_x" || tr.LastDetectionTime == "" {
		t.Errorf("headline fields not decoded: %+v", tr)
	}
	// Zero-count buckets omit detectionCount: 3 + 0 + 7.
	if got := tr.TotalDetections(); got != 10 {
		t.Errorf("TotalDetections = %d, want 10", got)
	}
}

func TestDecodeRuleTrends(t *testing.T) {
	trends, err := decodeRuleTrends(json.RawMessage(`{"ruleTrends":[{"ruleId":"ru_1"},{"ruleId":"ru_2"}]}`))
	if err != nil || len(trends) != 2 {
		t.Fatalf("decodeRuleTrends = %v, %v", trends, err)
	}
}

func TestModifyRulesRequiresRequests(t *testing.T) {
	c, err := NewClient(Settings{ProjectID: "p", Region: "us", CustomerID: "c"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ModifyRules(t.Context(), nil); err == nil {
		t.Error("empty batch must error client-side")
	}
}
