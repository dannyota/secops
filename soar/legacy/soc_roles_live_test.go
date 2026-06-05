package legacy

import (
	"testing"
)

// TestLiveSocRolesReads exercises the SOC Roles read endpoints (safe,
// read-only). Runs under SECOPS_SOAR_SMOKE=1.
//
// SocRoleList needs no arguments and is green on a tenant with no prior setup
// (a fresh tenant ships built-in roles; worst case the list is empty). When the
// list is non-empty we derive the first role's id and exercise the two
// id-keyed reads (SocRoleGet, SocRoleHasAssignedUsers) against it. The
// body-driven reads (SocRoleListByEnvironments) need a payload we cannot safely
// construct, so they are omitted. SOC roles are a permission/auth surface, so
// this file is reads-only by design — no CRUD lifecycle.
func TestLiveSocRolesReads(t *testing.T) {
	lc, ctx := liveClient(t)

	raw := readProbe(t, "socroles/GetSocRoles", func() (RawJSON, error) { return lc.SocRoleList(ctx) })
	if raw == nil {
		return // list errored; readProbe already reported it.
	}

	// Derive an id from the list so the id-keyed reads run only with a real id.
	roles := objects(t, "socroles list", raw, nil)
	if len(roles) == 0 {
		t.Log("socroles list empty; skipping id-keyed reads")
		return
	}
	id, ok := intField("id")(roles[0])
	if !ok {
		t.Log("first SOC role has no integer id; skipping id-keyed reads")
		return
	}

	readProbe(t, "socroles/GetSocRole/{id}", func() (RawJSON, error) { return lc.SocRoleGet(ctx, id) })
	readProbe(t, "socroles/HasAssignedUsers/{id}", func() (RawJSON, error) { return lc.SocRoleHasAssignedUsers(ctx, id) })
}
