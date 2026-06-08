package cli

import "testing"

// TestSurfaceRows checks the registry view assembles, that reconcile surfaces
// expose accurate prune capabilities (read offline with a nil client), and that
// non-reconcile surfaces carry none. It also smoke-tests that every surface
// builds offline without panicking.
func TestSurfaceRows(t *testing.T) {
	rows := surfaceRows()
	if len(rows) == 0 {
		t.Fatal("surfaceRows() returned nothing")
	}
	by := make(map[string]surfaceRow, len(rows))
	for _, r := range rows {
		by[r.Name] = r
	}

	// reference_lists: reconcile, NoDelete → not prune-eligible.
	if rl, ok := by["reference_lists"]; !ok {
		t.Error("reference_lists missing from surfaces")
	} else if rl.PruneEligible == nil || *rl.PruneEligible {
		t.Errorf("reference_lists prune_eligible = %v, want false", rl.PruneEligible)
	} else if rl.NoDelete == nil || !*rl.NoDelete {
		t.Errorf("reference_lists no_delete = %v, want true", rl.NoDelete)
	}

	// forwarders: reconcile, prune-eligible.
	if fw, ok := by["forwarders"]; !ok {
		t.Error("forwarders missing from surfaces")
	} else if fw.PruneEligible == nil || !*fw.PruneEligible {
		t.Errorf("forwarders prune_eligible = %v, want true", fw.PruneEligible)
	}

	// alerts: operational → no reconcile capabilities reported.
	if al, ok := by["alerts"]; !ok {
		t.Error("alerts missing from surfaces")
	} else if al.PruneEligible != nil {
		t.Error("alerts (operational) should report no prune capability")
	}
}
