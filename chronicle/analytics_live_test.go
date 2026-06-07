package chronicle_test

import (
	"errors"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveAnalyticsRead validates the Wave 17 read surfaces on the SIEM/ADC plane:
// investigations (Gemini TIN) + steps/comments, entity risk scores, BigQuery-export
// status, and MITRE coverage details. Read-only; gated on SECOPS_SIEM_SMOKE=1.
//
// These are Pre-GA / feature-gated (Enterprise Plus, TIN trial, …), so a 404/403
// is reported as "not enabled here" (a clean typed *APIError), not a failure; only
// a non-APIError — a decode/usage bug, which this test guards — fails.
func TestLiveAnalyticsRead(t *testing.T) {
	c, ctx := liveChronicle(t)
	report := func(name string, n int, err error) {
		if err == nil {
			t.Logf("OK %-26s %d", name, n)
			return
		}
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok {
			t.Logf("-- %-26s gated/unavailable: HTTP %d", name, ae.Status)
			return
		}
		t.Errorf("%s decode/usage bug: %v", name, err)
	}

	inv, err := c.ListInvestigations(ctx, 5)
	report("investigations", len(inv), err)
	if err == nil && len(inv) > 0 {
		id := inv[0].Name
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		steps, e := c.ListInvestigationSteps(ctx, id, 5)
		report("investigationSteps", len(steps), e)
		cm, e := c.ListInvestigationComments(ctx, id, 5)
		report("investigationComments", len(cm), e)
		if _, e := c.GetInvestigation(ctx, id); e != nil {
			report("getInvestigation", 0, e)
		} else {
			t.Logf("OK getInvestigation             1")
		}
	}

	rs, err := c.QueryEntityRiskScores(ctx, "", "", 5)
	report("entityRiskScores", len(rs), err)

	if _, err := c.GetBigQueryExport(ctx); err != nil {
		report("bigQueryExport", 0, err)
	} else {
		t.Logf("OK bigQueryExport              1")
	}

	cov, err := c.ListCoverageDetails(ctx, 5)
	report("coverageDetails", len(cov), err)
}
