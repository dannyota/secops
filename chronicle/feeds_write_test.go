package chronicle

import (
	"testing"

	"danny.vn/secops/auth"
)

// TestFeedLogType verifies the short→full log-type expansion the feeds write path
// requires (the API rejects a bare id as a "malformed resource name").
func TestFeedLogType(t *testing.T) {
	c, err := NewClient(Settings{ProjectID: "p", Region: "r", CustomerID: "c"}, auth.OAuth())
	if err != nil {
		t.Fatal(err)
	}
	full := "projects/p/locations/r/instances/c/logTypes/NGINX"
	if got := c.feedLogType("NGINX"); got != full {
		t.Errorf("feedLogType(short) = %q, want %q", got, full)
	}
	if got := c.feedLogType(full); got != full {
		t.Errorf("feedLogType(full) = %q, want it left unchanged", got)
	}
	if got := c.feedLogType(""); got != "" {
		t.Errorf("feedLogType(empty) = %q, want empty", got)
	}
}
