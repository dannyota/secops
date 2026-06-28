package soar_test

import "testing"

// TestLiveIntegrationConnectorDefsRead validates the v1alpha integration read path
// end-to-end: list integrations, then list one integration's connector definitions
// and GET one back (round-trip). Read-only.
func TestLiveIntegrationConnectorDefsRead(t *testing.T) {
	c, ctx := liveClient(t)

	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		t.Fatalf("ListIntegrations: %v", err)
	}
	if len(ints) == 0 {
		t.Skip("tenant has no integrations to exercise")
	}

	// Find any integration that has at least one connector definition.
	for _, in := range ints {
		defs, err := c.ListConnectors(ctx, in.Identifier)
		if err != nil {
			t.Fatalf("ListConnectors(%s): %v", in.Identifier, err)
		}
		if len(defs) == 0 {
			continue
		}
		got, err := c.GetConnectorDef(ctx, in.Identifier, defs[0].ID.String())
		if err != nil {
			t.Fatalf("GetConnectorDef(%s/%s): %v", in.Identifier, defs[0].ID, err)
		}
		if got.DisplayName != defs[0].DisplayName {
			t.Fatalf("round-trip mismatch: list %q vs get %q", defs[0].DisplayName, got.DisplayName)
		}
		return // one successful round-trip is enough
	}
	t.Skip("no integration with connector definitions on this tenant")
}
