package mirror

import (
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

// TestFederationGroupRoundTrips: a live group and a type-omitting on-disk file
// canonicalize equal (type defaulted), and the federated instances survive.
func TestFederationGroupRoundTrips(t *testing.T) {
	live, err := federationGroupObject(chronicle.FederationGroup{
		Name:               "projects/p/locations/r/instances/c/federationGroups/g1",
		DisplayName:        "apac",
		FederatedInstances: []string{"projects/p/locations/r/instances/sub1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.Slug != "apac" || live.ServerID == "" {
		t.Errorf("identity wrong: slug=%q id=%q", live.Slug, live.ServerID)
	}
	if !strings.Contains(string(live.Canonical), "FEDERATION_GROUP_TYPE_DEFAULT") {
		t.Errorf("type not defaulted into canonical:\n%s", live.Canonical)
	}
	dir := t.TempDir()
	if err := writeFederationGroupObject(dir, live); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadFederationGroups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || string(loaded[0].Canonical) != string(live.Canonical) {
		t.Errorf("round-trip mismatch:\n live=%s\n disk=%v", live.Canonical, loaded)
	}
}

// (IdP mapping groups moved to the SOAR plane — see soar/idp_mappings.go — after a
// live probe showed they 500 on the chronicle host but answer on siemplify-soar.)
