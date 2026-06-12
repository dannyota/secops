package chronicle

import (
	"encoding/json"
	"testing"
)

// The entity-summary API renders int64 counters as quoted strings (proto3 JSON);
// older/other shapes use bare numbers. flexInt must decode both, and an
// EntitySummary carrying string counts must not fail to unmarshal.
func TestFlexIntDecode(t *testing.T) {
	cases := []struct {
		in   string
		want flexInt
	}{
		{`"42"`, 42},
		{`42`, 42},
		{`"0"`, 0},
		{`0`, 0},
		{`""`, 0},
		{`null`, 0},
	}
	for _, tc := range cases {
		var n flexInt
		if err := json.Unmarshal([]byte(tc.in), &n); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc.in, err)
			continue
		}
		if n != tc.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", tc.in, n, tc.want)
		}
	}

	var bad flexInt
	if err := json.Unmarshal([]byte(`"nope"`), &bad); err == nil {
		t.Error("expected error decoding a non-numeric string, got nil")
	}
}

// alertCounts.count arrives as a string; the whole summary must still decode.
func TestEntitySummaryAlertCountStringCount(t *testing.T) {
	const payload = `{
		"alertCounts": [
			{"rule": "ru_test", "count": "9"},
			{"rule": "ru_two", "count": 3}
		],
		"widgetMetadata": {"total": "1000000000000", "detections": "5"},
		"prevalence": [{"count": "123456789012"}]
	}`
	var sum EntitySummary
	if err := json.Unmarshal([]byte(payload), &sum); err != nil {
		t.Fatalf("decode EntitySummary: %v", err)
	}
	if len(sum.AlertCounts) != 2 || sum.AlertCounts[0].Count != 9 || sum.AlertCounts[1].Count != 3 {
		t.Fatalf("AlertCounts = %+v", sum.AlertCounts)
	}
	if sum.WidgetMetadata == nil || sum.WidgetMetadata.Total != 1000000000000 {
		t.Fatalf("WidgetMetadata = %+v", sum.WidgetMetadata)
	}
	if len(sum.Prevalence) != 1 || sum.Prevalence[0].Count != 123456789012 {
		t.Fatalf("Prevalence = %+v", sum.Prevalence)
	}

	// flexInt marshals back as a plain JSON number.
	b, err := json.Marshal(sum.AlertCounts[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"rule":"ru_test","count":9}` {
		t.Errorf("re-marshal = %s", b)
	}
}
