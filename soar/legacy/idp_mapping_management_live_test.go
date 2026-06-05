package legacy

import (
	"testing"
)

// TestLiveIdpMappingReads: the IdpMappingManagement read surface (GET /idpMapping)
// returns HTTP 403 for the AppKey identity (permission-gated), so there is no
// green read-only probe to assert. The methods are exercised by request-building
// only. No CRUD test either — this is auth/idp config (excluded).
func TestLiveIdpMappingReads(t *testing.T) {
	t.Skip("IdpMappingManagement reads are 403 (permission-gated) for the AppKey identity")
}
