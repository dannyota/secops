package chronicle_test

import (
	"encoding/json"
	"testing"

	"danny.vn/secops/chronicle"
)

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
