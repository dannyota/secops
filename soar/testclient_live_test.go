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
// Gated on testing.Short and SECOPS_SOAR_SMOKE=1.
func liveClient(t *testing.T) (*soar.Client, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("live test")
	}
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
