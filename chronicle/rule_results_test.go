package chronicle

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDetectionMatchPreservesRaw locks the fix for the forgotten unmarshaler:
// the typed fields decode AND the full object (incl. rule-varying extras) is
// preserved in Raw.
func TestDetectionMatchPreservesRaw(t *testing.T) {
	in := `{"ruleId":"ru_1","ruleName":"x","alertState":"ALERTING",` +
		`"urlBackToProduct":"https://example.com/x","ruleLabels":[{"k":"v"}]}`
	var m DetectionMatch
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.RuleID != "ru_1" || m.AlertState != "ALERTING" {
		t.Errorf("typed fields lost: %+v", m)
	}
	if len(m.Raw) == 0 || !strings.Contains(string(m.Raw), "urlBackToProduct") {
		t.Errorf("Raw must preserve rule-varying fields; got %s", m.Raw)
	}
}
