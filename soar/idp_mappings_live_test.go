package soar_test

import (
	"testing"
)

// TestLiveIdpMappingGroupsRead validates the IdP-mapping read path on the SOAR
// host (these 500 on the chronicle host — a two-host surface). Read-only; gated on
// SECOPS_SOAR_SMOKE=1.
func TestLiveIdpMappingGroupsRead(t *testing.T) {
	c, ctx := liveClient(t)

	groups, err := c.ListIdpMappingGroups(ctx)
	if err != nil {
		t.Fatalf("ListIdpMappingGroups: %v", err)
	}
	t.Logf("OK ListIdpMappingGroups: %d group(s)", len(groups))

	if _, perr := c.GetIdpExternalProviders(ctx); perr != nil {
		t.Logf("-- GetIdpExternalProviders: %v", perr)
	} else {
		t.Logf("OK GetIdpExternalProviders")
	}
}
