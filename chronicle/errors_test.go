package chronicle

import (
	"net/http"
	"strings"
	"testing"
)

// TestRequestIDFromHeader: the first present request-id header wins, in priority
// order, and absence yields "".
func TestRequestIDFromHeader(t *testing.T) {
	h := http.Header{}
	if got := requestIDFromHeader(h); got != "" {
		t.Errorf("empty headers → %q, want \"\"", got)
	}
	h.Set("X-Request-Id", "req-2")
	h.Set("X-Goog-Request-Id", "goog-1")
	if got := requestIDFromHeader(h); got != "goog-1" {
		t.Errorf("priority: got %q, want goog-1", got)
	}
}

// TestAPIErrorIncludesRequestID: the request id appears in the error string when
// set, and is omitted otherwise.
func TestAPIErrorIncludesRequestID(t *testing.T) {
	with := (&APIError{
		Method:    "GET",
		URL:       "https://region-chronicle.googleapis.com/v1/projects/private/locations/us/instances/private/rules",
		Status:    500,
		Body:      "boom",
		RequestID: "rid-9",
	}).Error()
	if !strings.Contains(with, "request-id: rid-9") {
		t.Errorf("error string missing request id: %s", with)
	}
	if strings.Contains(with, "region-chronicle.googleapis.com") || strings.Contains(with, "/v1/projects/private") {
		t.Errorf("error string leaked URL: %s", with)
	}
	without := (&APIError{Method: "GET", URL: "u", Status: 500, Body: "boom"}).Error()
	if strings.Contains(without, "request-id") {
		t.Errorf("error string should omit request id when absent: %s", without)
	}
}
