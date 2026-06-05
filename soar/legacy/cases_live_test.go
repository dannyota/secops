package legacy

import (
	"testing"
)

// TestLiveCasesCoreReads exercises the zero-argument, no-setup-required reads on
// the legacy case-queue surface (path substrings /cases/ and /cases-queue/).
// Both endpoints return safely on a tenant with no prior case setup. Runs under
// SECOPS_SOAR_SMOKE=1.
//
// Excluded by design: every other case read needs a specific SOAR integer case
// id, a string result/evidence id, or a POST filter body that cannot be safely
// constructed without prior tenant state (GetCaseExists/GetCaseWall/
// GetCaseInsights/GetCaseFullDetails/GetActionResult/GetEvidenceData,
// IsCaseUpdated/GetWorkflowInstanceSummary/GetAlertNames/ListCaseCards).
func TestLiveCasesCoreReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "cases-queue/ListAlertVendors", func() (RawJSON, error) { return lc.ListAlertVendors(ctx) })
	readProbe(t, "cases-queue/ListSavedFilters", func() (RawJSON, error) { return lc.ListSavedFilters(ctx) })
}
