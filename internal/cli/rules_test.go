package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTimeWindow(t *testing.T) {
	start, end := timeWindow(24)
	if d := end.Sub(start); d != 24*time.Hour {
		t.Errorf("window = %v, want 24h", d)
	}
	// Non-positive falls back to 24h.
	if start, end := timeWindow(0); end.Sub(start) != 24*time.Hour {
		t.Errorf("default window != 24h")
	}
	if start, end := timeWindow(168); end.Sub(start) != 168*time.Hour {
		t.Errorf("168h window wrong")
	}
}

func TestHoursOrDefault(t *testing.T) {
	if hoursOrDefault(0) != 24 || hoursOrDefault(-5) != 24 {
		t.Error("non-positive should default to 24")
	}
	if hoursOrDefault(72) != 72 {
		t.Error("positive should pass through")
	}
}

func TestOrFirst(t *testing.T) {
	if orFirst("", "b") != "b" || orFirst("a", "b") != "a" || orFirst("", "") != "" {
		t.Error("orFirst wrong")
	}
}

func TestWriteRulesAlerts(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRulesAlerts(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no alerts.") {
		t.Errorf("empty alerts: %q", buf.String())
	}

	buf.Reset()
	alerts := []json.RawMessage{json.RawMessage(`{"ruleMetadata":{"x":1}}`)}
	if err := writeRulesAlerts(&buf, alerts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ruleMetadata") {
		t.Errorf("alert body not emitted: %q", buf.String())
	}
}
