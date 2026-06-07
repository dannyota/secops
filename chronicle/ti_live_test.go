package chronicle_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
)

// liveChronicle builds a SIEM client from the local instance config + ADC.
// Gated on SECOPS_SIEM_SMOKE=1.
func liveChronicle(t *testing.T) (*chronicle.Client, context.Context) {
	t.Helper()
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

// TestLiveThreatCollectionsRead validates the Threat-Intel read path end to end:
// list campaigns+reports, then GET one back (round-trip). Read-only. Also logs the
// raw top-level field set so the typed struct can be refined.
func TestLiveThreatCollectionsRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	tcs, err := c.ListThreatCollections(ctx, chronicle.ThreatCollectionQuery{
		Types:    []string{chronicle.CollectionCampaign, chronicle.CollectionReport},
		OrderBy:  "last_modification_date-",
		PageSize: 10,
		MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("ListThreatCollections: %v", err)
	}
	t.Logf("listed %d threat collection(s)", len(tcs))
	if len(tcs) == 0 {
		t.Skip("tenant has no campaign/report collections to round-trip")
	}

	// Field-set of the first item, to refine the typed struct.
	var m map[string]json.RawMessage
	_ = json.Unmarshal(tcs[0].Raw, &m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	t.Logf("first: id=%q type=%q displayName=%q name=%q", tcs[0].ID, tcs[0].Type, tcs[0].DisplayName, tcs[0].Name)
	t.Logf("top-level keys: %v", keys)

	id := tcs[0].ID
	if id == "" {
		id = tcs[0].Name
	}
	got, err := c.GetThreatCollection(ctx, id)
	if err != nil {
		t.Fatalf("GetThreatCollection(%q): %v", id, err)
	}
	if got.ID == "" && got.Name == "" {
		t.Fatalf("GetThreatCollection returned an empty object")
	}
}
