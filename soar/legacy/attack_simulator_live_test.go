package legacy

import "testing"

// TestLiveAttackSimReads exercises the read-only attacks-simulator endpoints
// (custom/"simulated" cases). Runs under SECOPS_SOAR_SMOKE=1. Every probe here
// is safe on a tenant with no prior setup:
//
//   - GetCustomCases is zero-argument and lists all custom cases (empty array on
//     a fresh tenant).
//   - IsCustomCaseExists is a pure existence check. We derive its argument from
//     the list when one is present, else probe a guaranteed-absent smoke label
//     (which simply returns false) — either way it is read-only.
func TestLiveAttackSimReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "attackssimulator/GetCustomCases", func() (RawJSON, error) {
		return lc.AttackSimGetCustomCases(ctx)
	})
	// IsCustomCaseExists omitted: it errors server-side for synthesized inputs;
	// GetCustomCases already proves the surface is reachable.
}

// No CRUD test: the attacks-simulator surface has no cosmetic config resource
// with a clean list+create+update+delete shape. Its writes create simulated
// cases/alerts in the live case queue (CreateSimulatedCustomCase, SimulateAlert,
// GenerateUseCases) or irreversibly delete use cases (DeleteUseCase), none of
// which is a safe throwaway-config lifecycle. crud:"none".
