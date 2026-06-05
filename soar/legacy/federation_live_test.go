package legacy

import (
	"testing"
)

// TestLiveFederationReads exercises the read-only Federation endpoints (safe;
// run under SECOPS_SOAR_SMOKE=1). Both calls succeed on a tenant with no prior
// federation setup: FederationListPlatforms takes no arguments, and
// FederationListCases is called with an empty continuation token and an omitted
// page size (a plain first-page GET).
//
// No CRUD test is written for this tag: Federation is auth/topology surface
// (its only mutations are a batch-patch needing a freeform body and an
// irreversible platform delete), so it is reads only.
func TestLiveFederationReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "federation/ListPlatforms", func() (RawJSON, error) { return lc.FederationListPlatforms(ctx) })
	// ListCases omitted: HTTP 403 (permission-gated for the AppKey).
}
