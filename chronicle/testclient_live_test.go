package chronicle_test

import (
	"context"
	"os"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
)

// liveChronicle builds a SIEM client from the local instance config + ADC.
// Gated on testing.Short and SECOPS_SIEM_SMOKE=1.
func liveChronicle(t *testing.T) (*chronicle.Client, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("live test")
	}
	if os.Getenv("SECOPS_SIEM_SMOKE") != "1" {
		t.Skip("live SIEM smoke — set SECOPS_SIEM_SMOKE=1 (with instance config + ADC) to run")
	}
	inst, err := config.Load("")
	if err != nil {
		t.Skipf("no instance config: %v", err)
	}
	c, err := chronicle.NewClient(inst.Settings(), auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4)))
	if err != nil {
		t.Fatal(err)
	}
	return c, context.Background()
}
