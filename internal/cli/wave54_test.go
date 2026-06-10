package cli

import (
	"encoding/json"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestEmitTrendRowsSortsNoisiestFirst(t *testing.T) {
	mk := func(body string) chronicle.RuleTrend {
		var tr chronicle.RuleTrend
		if err := json.Unmarshal([]byte(body), &tr); err != nil {
			t.Fatal(err)
		}
		return tr
	}
	trends := []chronicle.RuleTrend{
		mk(`{"ruleId":"ru_quiet","detectionCounts":[]}`),
		mk(`{"ruleId":"ru_noisy","detectionCounts":[{"detectionCount":9}]}`),
		mk(`{"ruleId":"ru_mid","detectionCounts":[{"detectionCount":2},{"detectionCount":1}]}`),
	}
	rows := emitTrendRows(trends, map[string]string{"ru_noisy": "Noisy Rule"})
	if rows[0].RuleID != "ru_noisy" || rows[1].RuleID != "ru_mid" || rows[2].RuleID != "ru_quiet" {
		t.Errorf("sort order wrong: %+v", rows)
	}
	if rows[0].DisplayName != "Noisy Rule" || rows[0].Detections != 9 {
		t.Errorf("row fields wrong: %+v", rows[0])
	}
}

func TestSlicesChunk(t *testing.T) {
	var got [][]string
	for chunk := range slicesChunk([]string{"a", "b", "c", "d", "e"}, 2) {
		got = append(got, chunk)
	}
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 || got[2][0] != "e" {
		t.Errorf("chunks = %v", got)
	}
}
