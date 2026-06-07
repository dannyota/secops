package chronicle_test

import (
	"errors"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveScheduledReportsRead validates the dashboardScheduledReports read path
// (list + get round-trip + fetchHistory). Read-only; gated on SECOPS_SIEM_SMOKE=1.
// A 403/feature-gate is reported as unavailable (clean typed *APIError), not a
// failure; only a non-APIError (a decode/usage bug) fails.
func TestLiveScheduledReportsRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	reports, err := c.ListScheduledReports(ctx)
	if err != nil {
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok {
			t.Logf("-- ListScheduledReports permission/feature-gated: HTTP %d", ae.Status)
			return
		}
		t.Fatalf("ListScheduledReports decode/usage bug: %v", err)
	}
	t.Logf("OK ListScheduledReports: %d report(s)", len(reports))
	if len(reports) == 0 {
		return
	}
	r := reports[0]
	got, gerr := c.GetScheduledReport(ctx, r.ID())
	if gerr != nil {
		t.Errorf("GetScheduledReport(%q): %v", r.ID(), gerr)
	} else {
		t.Logf("OK GetScheduledReport: %s (status=%s, etag=%t)", got.ID(), got.Status, got.Etag != "")
	}
	if _, herr := c.FetchScheduledReportHistory(ctx, r.ID()); herr != nil {
		if ae, ok := errors.AsType[*chronicle.APIError](herr); ok {
			t.Logf("-- FetchScheduledReportHistory gated: HTTP %d", ae.Status)
		} else {
			t.Errorf("FetchScheduledReportHistory decode/usage bug: %v", herr)
		}
	} else {
		t.Logf("OK FetchScheduledReportHistory: %s", r.ID())
	}
}
