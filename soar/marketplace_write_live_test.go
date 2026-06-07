package soar_test

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMarketplaceInstallWriteSmoke validates the Content-Hub install/uninstall
// path (Wave 11) on a genuinely-not-installed, inert utility pack (ImageUtilities):
// confirm not installed → install → confirm installed → uninstall → confirm gone.
// Install adds only (unconfigured) action definitions — no integration instance is
// created, so nothing runs or connects to anything. Install state is read from the
// installed-packs list (the marketplace `isInstalled` flag is not populated on this
// tenant), with polling because install/uninstall → list has indexing lag. Cleanup
// retries uninstall and FAILS LOUDLY if residue remains, so a stuck install can't
// silently linger on the production Content Hub. Gated on SECOPS_SOAR_SMOKE=1 +
// SECOPS_SOAR_SMOKE_WRITE=1.
func TestLiveMarketplaceInstallWriteSmoke(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE_WRITE=1 to run (installs + uninstalls a throwaway Content-Hub pack)")
	}
	const target = "ImageUtilities"

	isInstalled := func() bool {
		ints, err := c.ListIntegrations(ctx)
		if err != nil {
			t.Fatalf("list integrations: %v", err)
		}
		for _, in := range ints {
			if in.ProdIdentifier == target || strings.SplitN(in.Identifier, "__", 2)[0] == target {
				return true
			}
		}
		return false
	}
	// reached polls until isInstalled()==want (install/uninstall → list lags).
	reached := func(want bool) bool {
		for range 15 {
			if isInstalled() == want {
				return true
			}
			time.Sleep(time.Second)
		}
		return isInstalled() == want
	}

	if isInstalled() {
		t.Skipf("%s already installed — choose a different throwaway target", target)
	}

	// Always leave it uninstalled; fail loudly if residue remains.
	t.Cleanup(func() {
		if isInstalled() {
			_, _ = c.UninstallMarketplaceIntegration(ctx, target, map[string]any{})
			if !reached(false) {
				t.Errorf("RESIDUE: %s still installed after cleanup — uninstall it manually in the Content Hub", target)
			}
		}
	})

	if _, err := c.InstallMarketplaceIntegration(ctx, target, map[string]any{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !reached(true) {
		t.Fatalf("%s did not appear installed after install", target)
	}

	if _, err := c.UninstallMarketplaceIntegration(ctx, target, map[string]any{}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !reached(false) {
		t.Errorf("%s still installed after uninstall", target)
	}
}
