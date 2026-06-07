package chronicle_test

import (
	"os"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveCuratedBatchToggleWriteSmoke validates BatchUpdateCuratedRuleSetDeployments
// (Wave 9) by toggling ONE currently-disabled ("unused") curated deployment on and
// then back off, asserting the live state changed and restored. Careful by design:
// it only touches a deployment that is already enabled=false AND alerting=false, it
// enables WITHOUT alerting (no alerts generated in the brief window), and a cleanup
// restores it to disabled even on failure. Gated on SECOPS_SIEM_SMOKE=1 +
// SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveCuratedBatchToggleWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (toggles one disabled curated deployment live, self-restoring)")
	}

	deps, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}

	// Pick a deployment that is currently OFF (unused) with a valid precision.
	var cat, set, prec string
	found := false
	for _, d := range deps {
		if d.Enabled || d.Alerting {
			continue
		}
		cc, ss, pp, perr := chronicle.ParseCuratedDeploymentName(d.Name)
		if perr != nil || (pp != "precise" && pp != "broad") {
			continue
		}
		cat, set, prec, found = cc, ss, pp, true
		break
	}
	if !found {
		t.Skip("no disabled curated deployment available to safely toggle")
	}
	t.Logf("toggling unused deployment cat=%s set=%s precision=%s", cat, set, prec)

	set2 := func(enabled bool) error {
		_, e := c.BatchUpdateCuratedRuleSetDeployments(ctx, []chronicle.CuratedDeploymentChange{
			{CategoryID: cat, RuleSetID: set, Precision: prec, Enabled: enabled, Alerting: false},
		})
		return e
	}
	// Safety net: always restore to disabled.
	t.Cleanup(func() { _ = set2(false) })

	verify := func(want bool) {
		t.Helper()
		ds, e := c.ListCuratedRuleSetDeployments(ctx)
		if e != nil {
			t.Fatalf("re-list: %v", e)
		}
		for _, d := range ds {
			cc, ss, pp, _ := chronicle.ParseCuratedDeploymentName(d.Name)
			if cc == cat && ss == set && pp == prec {
				if d.Enabled != want {
					t.Errorf("enabled=%v, want %v", d.Enabled, want)
				}
				return
			}
		}
		t.Errorf("target deployment not found on re-list")
	}

	// Enable via the batch primitive → verify → disable (restore) → verify.
	if err := set2(true); err != nil {
		t.Fatalf("batch enable: %v", err)
	}
	verify(true)
	if err := set2(false); err != nil {
		t.Fatalf("batch disable (restore): %v", err)
	}
	verify(false)
}
