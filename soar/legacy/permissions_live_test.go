package legacy

import "testing"

// TestLivePermissionsReads exercises the read-only permission-group endpoints
// (safe; no prior tenant setup required). Runs under SECOPS_SOAR_SMOKE=1.
//
// The first three are zero-argument list/status reads. The last one derives its
// argument in-test: it lists the group cards, takes the first card's id, then
// fetches that one group by id — so it only runs when the tenant actually has a
// permission group, and never guesses an id.
//
// No CRUD test is provided: permission groups are role-based access control
// (auth/permissions), which is explicitly off-limits for live mutation tests.
func TestLivePermissionsReads(t *testing.T) {
	lc, ctx := liveClient(t)

	readProbe(t, "permissions/ListGroupCards", func() (RawJSON, error) {
		return lc.PermissionListGroupCards(ctx)
	})
	readProbe(t, "permissions/ListGroupTypes", func() (RawJSON, error) {
		return lc.PermissionListGroupTypes(ctx)
	})
	readProbe(t, "permissions/GetAllEnvironmentStatus", func() (RawJSON, error) {
		return lc.PermissionGetAllEnvironmentStatus(ctx)
	})

	// Derived read: list cards, take the first group's id, then Get it.
	raw, err := lc.PermissionListGroupCards(ctx)
	cards := objects(t, "permissions/ListGroupCards", raw, err)
	if len(cards) == 0 {
		t.Skip("no permission groups on this tenant; skipping derived Get-by-id read")
	}
	id, ok := intField("id")(cards[0])
	if !ok {
		t.Skip("first permission group card has no integer id; skipping derived Get-by-id read")
	}
	readProbe(t, "permissions/Get(firstID)", func() (RawJSON, error) {
		return lc.PermissionGet(ctx, id)
	})
}
