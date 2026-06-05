package legacy

import (
	"encoding/json"
	"testing"
)

// TestLiveIntegrationsReads exercises the integrations read surface that is safe
// on a tenant with NO prior setup. Only ListInstalledIntegrations is a true
// zero-argument read; the Get* endpoints all require a specific integration or
// instance identifier (and GetIntegrationDefaultInstance can 404 when an
// integration has no default instance), so they are intentionally excluded to
// keep this probe green. The Post* "list" endpoints take freeform filter bodies
// we cannot safely construct, so they are excluded too. Runs under
// SECOPS_SOAR_SMOKE=1.
func TestLiveIntegrationsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "integrations/GetInstalledIntegrations", func() (RawJSON, error) {
		return lc.ListInstalledIntegrations(ctx)
	})
}

// GROUP E (operational config) — integration instances. A new instance of an
// already-installed integration starts UNCONFIGURED (no credentials), so it runs
// no actions and is inert until configured. CreateIntegrationInstance returns the
// new instance object (with its identifier), so cleanup is exact. There is no
// update endpoint for an instance, so the lifecycle is create -> verify -> delete
// -> verify gone rather than full CRUD.

// TestLiveIntegrationInstanceCRUD creates a throwaway, unconfigured instance of an
// installed integration in one real environment, verifies it appears, then deletes
// it. Write-gated; deletes the instance on cleanup even on failure.
func TestLiveIntegrationInstanceCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)
	env := firstEnvironment(t, ctx, lc)

	raw, err := lc.ListInstalledIntegrations(ctx)
	installed := objects(t, "installed-integrations", raw, err)
	if len(installed) == 0 {
		t.Skip("no installed integrations to instantiate")
	}
	integ := strField("identifier")(installed[0])
	if integ == "" {
		t.Skip("first installed integration has no identifier")
	}

	// Create. Some integrations are singletons (only a default instance allowed);
	// treat that rejection as an environmental skip, not a failure.
	raw, err = lc.CreateIntegrationInstance(ctx, map[string]any{
		"integrationIdentifier": integ, "environment": env,
	})
	if err != nil {
		t.Skipf("integration %q does not allow a new instance here: %v", integ, err)
	}
	var inst map[string]any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	id := strField("identifier")(inst)
	if id == "" {
		t.Fatalf("created instance has no identifier (resp: %s)", shapeOf(raw))
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if _, err := lc.DeleteIntegrationInstance(ctx, inst); err != nil {
			t.Logf("cleanup: could not delete throwaway integration instance %q: %v", id, err)
		}
	})

	// Verify it appears among the integration's instances for this environment.
	raw, err = lc.ListOptionalIntegrationInstances(ctx, map[string]any{
		"environments": []any{env}, "integrationIdentifier": integ,
	})
	found := findBy(objects(t, "optional-instances", raw, err), func(o map[string]any) bool {
		return strField("identifier")(o) == id
	})
	if found == nil {
		t.Fatalf("created instance %q not found in optional instances", id)
	}

	// Delete.
	if _, err := lc.DeleteIntegrationInstance(ctx, inst); err != nil {
		t.Fatalf("delete integration instance: %v", err)
	}
	deleted = true

	// Verify gone.
	raw, err = lc.ListOptionalIntegrationInstances(ctx, map[string]any{
		"environments": []any{env}, "integrationIdentifier": integ,
	})
	if gone := findBy(objects(t, "optional-instances#2", raw, err), func(o map[string]any) bool {
		return strField("identifier")(o) == id
	}); gone != nil {
		t.Fatalf("integration instance %q still present after delete", id)
	}
}
