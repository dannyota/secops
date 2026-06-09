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

func TestRuleErrorUnmarshalStringError(t *testing.T) {
	var got RuleError
	if err := json.Unmarshal([]byte(`{
		"name":"projects/p/locations/us/instances/i/ruleExecutionErrors/e1",
		"error":"invalid rule query",
		"category":"RULES_EXECUTION_ERROR"
	}`), &got); err != nil {
		t.Fatalf("unmarshal rule error: %v", err)
	}
	if got.Message() != "invalid rule query" {
		t.Fatalf("Message() = %q", got.Message())
	}
	if len(got.ErrorObject) != 0 {
		t.Fatalf("ErrorObject set for string error: %s", got.ErrorObject)
	}
	if len(got.Raw) == 0 {
		t.Fatal("Raw was not preserved")
	}
}

func TestRuleErrorUnmarshalStructuredError(t *testing.T) {
	var got RuleError
	raw := []byte(`{
		"name":"projects/p/locations/us/instances/i/ruleExecutionErrors/e1",
		"error":{
			"@type":"type.googleapis.com/example.RuleError",
			"message":"invalid event selector",
			"details":[{"field":"events.principal.hostname"}]
		},
		"category":"RULES_EXECUTION_ERROR"
	}`)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal rule error: %v", err)
	}
	if want := "type.googleapis.com/example.RuleError: invalid event selector (events.principal.hostname)"; got.Message() != want {
		t.Fatalf("Message() = %q, want %q", got.Message(), want)
	}
	if len(got.ErrorObject) == 0 {
		t.Fatal("ErrorObject was not preserved")
	}
	if len(got.Raw) == 0 {
		t.Fatal("Raw was not preserved")
	}

	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal rule error: %v", err)
	}
	s := string(out)
	for _, want := range []string{`"error_object":`, `"raw":`, `"message":"invalid event selector"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("marshaled JSON %s does not contain %s", s, want)
		}
	}
}
