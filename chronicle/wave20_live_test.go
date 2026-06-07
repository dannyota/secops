package chronicle_test

import (
	"errors"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveWave20Read validates the read paths of the Wave 20 MSSP/federation
// surfaces: federationGroups, legacySoarIdpMappingGroups, tenants, and the
// multitenant directory. Read-only; gated on SECOPS_SIEM_SMOKE=1. These are
// multi-tenant features, so on a single-tenant instance the lists are typically
// empty (or 403/feature-gated). A clean *APIError is reported, not a failure;
// only a non-APIError (a decode/usage bug) fails.
func TestLiveWave20Read(t *testing.T) {
	c, ctx := liveChronicle(t)
	report := func(name string, n int, err error) {
		if err == nil {
			t.Logf("OK %-28s %d", name, n)
			return
		}
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok {
			t.Logf("-- %-28s permission/feature-gated: HTTP %d", name, ae.Status)
			return
		}
		t.Errorf("%s decode/usage bug: %v", name, err)
	}

	groups, err := c.ListFederationGroups(ctx)
	report("federationGroups", len(groups), err)

	// IdP mapping groups are a SOAR-host surface (they 500 on chronicle) — read-
	// validated on the SOAR plane in soar/idp_mappings_live_test.go, not here.

	tenants, err := c.ListTenants(ctx)
	report("tenants", len(tenants), err)

	if _, derr := c.GetMultitenantDirectory(ctx); derr != nil {
		report("multitenantDirectory", 0, derr)
	} else {
		t.Logf("OK %-28s ok", "multitenantDirectory")
	}
}
