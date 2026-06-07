package chronicle_test

import (
	"errors"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveMetricDefinitionsRead validates the metricDefinitions read path (list +
// get round-trip). The v1alpha method set is create/get/list/patch (no delete).
// Read-only; gated on SECOPS_SIEM_SMOKE=1. A 403 is reported as "permission-gated
// on this identity" (a clean typed *APIError), not a failure — only a non-APIError
// (a decode/usage bug, which this test guards) fails.
func TestLiveMetricDefinitionsRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	defs, err := c.ListMetricDefinitions(ctx)
	if err != nil {
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok {
			t.Logf("-- ListMetricDefinitions permission/feature-gated: HTTP %d", ae.Status)
			return
		}
		t.Fatalf("ListMetricDefinitions decode/usage bug: %v", err)
	}
	t.Logf("OK ListMetricDefinitions: %d metric(s)", len(defs))
	if len(defs) > 0 {
		if _, gerr := c.GetMetricDefinition(ctx, defs[0].ID()); gerr != nil {
			t.Errorf("GetMetricDefinition(%q): %v", defs[0].ID(), gerr)
		} else {
			t.Logf("OK GetMetricDefinition: %s (state=%s)", defs[0].ID(), defs[0].State)
		}
	}
}
