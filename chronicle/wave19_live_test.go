package chronicle_test

import (
	"errors"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveWave19Read validates the read paths of the Wave 19 enrichment/ingestion-
// governance surfaces: dataTaps, errorNotificationConfigs, enrichmentControls.
// Read-only; gated on SECOPS_SIEM_SMOKE=1. A 403/feature-gate is reported as
// unavailable (clean typed *APIError), not a failure; only a non-APIError (a
// decode/usage bug) fails.
func TestLiveWave19Read(t *testing.T) {
	c, ctx := liveChronicle(t)
	report := func(name string, n int, err error) {
		if err == nil {
			t.Logf("OK %-26s %d", name, n)
			return
		}
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok {
			t.Logf("-- %-26s permission/feature-gated: HTTP %d", name, ae.Status)
			return
		}
		t.Errorf("%s decode/usage bug: %v", name, err)
	}

	taps, err := c.ListDataTaps(ctx)
	report("dataTaps", len(taps), err)
	if err == nil && len(taps) > 0 {
		if _, gerr := c.GetDataTap(ctx, taps[0].ID()); gerr != nil {
			report("getDataTap", 0, gerr)
		}
	}

	cfgs, err := c.ListErrorNotificationConfigs(ctx)
	report("errorNotificationConfigs", len(cfgs), err)
	if err == nil && len(cfgs) > 0 {
		if _, gerr := c.GetErrorNotificationConfig(ctx, cfgs[0].ID()); gerr != nil {
			report("getErrorNotificationConfig", 0, gerr)
		}
	}

	controls, err := c.ListEnrichmentControls(ctx)
	report("enrichmentControls", len(controls), err)
	if err == nil && len(controls) > 0 {
		if _, gerr := c.GetEnrichmentControl(ctx, controls[0].ID()); gerr != nil {
			report("getEnrichmentControl", 0, gerr)
		}
	}
}
