package legacy

import (
	"testing"
)

// TestLiveDynamicCasesReads would exercise the /dynamic-cases/ read endpoints,
// but every read in this tag needs a specific identifier or body that cannot be
// supplied on a tenant with no prior setup:
//
//   - GET endpoints all require an argument (DynamicGetCaseDetails(caseID),
//     DynamicGetEvidenceData(evidenceID), GetWallActivities*(caseID),
//     DynamicCaseXGetWallActivitiesForCommandCenter(id)).
//   - POST endpoints are case/alert mutations or searches whose bodies reference
//     live case IDs / alert identifiers (GetCaseWallActivities, GetAlertEvents,
//     IsCaseUpdated, ...).
//
// There is no zero-argument List* in this tag to derive an id from, so there is
// nothing that is guaranteed green with no setup. The test still calls
// liveClient so the file compiles and the gate is honored, then skips.
func TestLiveDynamicCasesReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
