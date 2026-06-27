package cli

import (
	"testing"
	"time"
)

// TestResolveWindow locks the shared --hours / --from / --to window resolution used
// by `search udm` and `search raw`.
func TestResolveWindow(t *testing.T) {
	// --hours: a 2h window ending ~now.
	start, end, err := resolveWindow(2, "", "")
	if err != nil {
		t.Fatalf("hours: %v", err)
	}
	if d := end.Sub(start); d != 2*time.Hour {
		t.Errorf("window = %v, want 2h", d)
	}
	if time.Since(end) > time.Minute {
		t.Errorf("end should be ~now, got %v", end)
	}

	// explicit --from/--to override --hours.
	s, e, err := resolveWindow(24, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("from/to: %v", err)
	}
	if !s.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("from/to = %v .. %v", s, e)
	}

	// start >= end is an error.
	if _, _, err := resolveWindow(24, "2024-01-02T00:00:00Z", "2024-01-01T00:00:00Z"); err == nil {
		t.Error("expected error when start >= end")
	}
	// invalid timestamp is an error.
	if _, _, err := resolveWindow(24, "not-a-time", ""); err == nil {
		t.Error("expected error for invalid --from")
	}
}
