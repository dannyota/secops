package cli

import (
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestHintDuplicateDashboard adds the actionable hint only on the known
// server-side 500, and passes other errors through untouched.
func TestHintDuplicateDashboard(t *testing.T) {
	dup := &chronicle.APIError{Status: http.StatusInternalServerError, Body: `{"error":{"message":"error duplicating Native Dashboards"}}`}
	if got := hintDuplicateDashboard(dup); !strings.Contains(got.Error(), "recreate it instead") {
		t.Errorf("500 duplicate error should gain the recreate hint; got %v", got)
	}
	notFound := &chronicle.APIError{Status: http.StatusNotFound, Body: "nope"}
	if got := hintDuplicateDashboard(notFound); strings.Contains(got.Error(), "recreate it instead") {
		t.Errorf("a 404 should pass through without the hint; got %v", got)
	}
}
