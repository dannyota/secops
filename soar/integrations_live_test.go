package soar_test

import (
	"context"
	"os"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/soar"
)

// liveClient builds a modern SOAR client from the local instance config + AppKey.
// Gated: the caller must have set SECOPS_SOAR_SMOKE=1.
func liveClient(t *testing.T) (*soar.Client, context.Context) {
	t.Helper()
	if os.Getenv("SECOPS_SOAR_SMOKE") != "1" {
		t.Skip("live SOAR smoke — set SECOPS_SOAR_SMOKE=1 (with instance config + AppKey) to run")
	}
	inst, err := config.Load("")
	if err != nil {
		t.Skipf("no instance config: %v", err)
	}
	key := inst.SOARAppKey
	if key == "" {
		key = auth.FromEnv("SECOPS_SOAR_APP_KEY", "SECOPS_API_KEY")
	}
	if inst.SOARURL == "" || key == "" {
		t.Skip("soar_url and/or SOAR AppKey not configured")
	}
	c, err := soar.NewClient(soar.Settings{
		BaseURL:       inst.SOARURL,
		ProjectNumber: inst.ProjectNumberString(),
		Region:        inst.Region,
		CustomerID:    inst.CustomerID,
		ForceIPv4:     inst.ForceIPv4,
	}, auth.SOARAppKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return c, context.Background()
}

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
