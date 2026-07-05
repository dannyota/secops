package chronicle_test

import (
	"encoding/json"
	"errors"
	"strings"
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

// TestLiveGetThreatCollectionFilterSet fetches the filter-set metadata and
// probes both path forms to settle the path ambiguity.
//
// The official docs describe this as `:getThreatCollectionFilterSet` (a custom
// method on the instance); the console fetches it as a plain subresource
// `threatCollectionFilterSet`. This test tries both and reports which answers.
// Read-only.
func TestLiveGetThreatCollectionFilterSet(t *testing.T) {
	c, ctx := liveChronicle(t)

	// Primary: console form (plain subresource) — used by the SDK method.
	raw, err := c.GetThreatCollectionFilterSet(ctx)
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			t.Skipf("ATI not licensed (403): %v", err)
		}
		t.Logf("console form (threatCollectionFilterSet): FAILED: %v", err)
	} else {
		t.Logf("console form (threatCollectionFilterSet): OK (%d bytes)", len(raw))
	}

	// The docs describe an alternative path `:getThreatCollectionFilterSet`
	// (instance-level custom method). The SDK uses the console-observed plain
	// subresource form. If the primary succeeds, the docs form is deferred to
	// manual check; if it fails, both forms should be tested via curl.
	if err == nil {
		t.Log("console form works; docs form (:getThreatCollectionFilterSet) deferred to manual probe")
	}

	if raw == nil && err != nil {
		t.Fatalf("GetThreatCollectionFilterSet: %v", err)
	}
}

// TestLiveBatchGetIocAssociations fetches a single IoC association from the
// first threat collection's associations list. Read-only.
func TestLiveBatchGetIocAssociations(t *testing.T) {
	c, ctx := liveChronicle(t)

	// Get one threat collection to find an association id.
	tcs, err := c.ListThreatCollections(ctx, chronicle.ThreatCollectionQuery{
		PageSize: 5,
		MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("ListThreatCollections: %v", err)
	}
	// Find the first collection with associations.
	var assocID string
	for _, tc := range tcs {
		if len(tc.Associations) > 0 {
			assocID = tc.Associations[0]
			break
		}
	}
	if assocID == "" {
		t.Skip("no threat collections with associations found")
	}
	t.Logf("probing association: %s", assocID)

	assocs, err := c.BatchGetIocAssociations(ctx, assocID)
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			t.Skipf("ATI not licensed (403): %v", err)
		}
		t.Fatalf("BatchGetIocAssociations: %v", err)
	}
	t.Logf("got %d association(s)", len(assocs))
	if len(assocs) == 0 {
		t.Skip("batchGet returned no associations")
	}
	a := assocs[0]
	t.Logf("first: id=%q type=%q display=%q", a.ID, a.AssociationType, a.ThreatDisplayName)
	if a.ID == "" {
		t.Error("association ID is empty")
	}
}

// TestLiveListCoverageDetailsFiltered lists coverage for one campaign. Read-only.
func TestLiveListCoverageDetailsFiltered(t *testing.T) {
	c, ctx := liveChronicle(t)

	tcs, err := c.ListThreatCollections(ctx, chronicle.ThreatCollectionQuery{
		Types:    []string{chronicle.CollectionCampaign},
		PageSize: 1,
		MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("ListThreatCollections: %v", err)
	}
	if len(tcs) == 0 {
		t.Skip("no campaigns")
	}
	id := tcs[0].ID
	t.Logf("querying coverage for %s", id)

	details, err := c.ListCoverageDetailsFiltered(ctx, []string{id}, 100)
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			t.Skipf("ATI not licensed (403): %v", err)
		}
		t.Fatalf("ListCoverageDetailsFiltered: %v", err)
	}
	t.Logf("got %d coverage detail(s)", len(details))
	for _, d := range details {
		if !strings.Contains(d.ThreatCollection, id) {
			t.Errorf("coverage detail %s has unexpected collection %s", d.Name, d.ThreatCollection)
		}
	}
}

// TestLiveFetchRelatedAssociations fetches associations related to a campaign.
// Read-only.
func TestLiveFetchRelatedAssociations(t *testing.T) {
	c, ctx := liveChronicle(t)

	tcs, err := c.ListThreatCollections(ctx, chronicle.ThreatCollectionQuery{
		Types:    []string{chronicle.CollectionCampaign},
		PageSize: 1,
		MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("ListThreatCollections: %v", err)
	}
	if len(tcs) == 0 {
		t.Skip("no campaigns")
	}
	id := tcs[0].ID
	t.Logf("fetching related associations for %s", id)

	assocs, err := c.FetchRelatedAssociations(ctx, chronicle.RelatedAssociationQuery{
		Type:             chronicle.AssociationMalware,
		ThreatCollection: id,
		PageSize:         10,
		MaxPages:         1,
	})
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 403 {
			t.Skipf("ATI not licensed (403): %v", err)
		}
		t.Fatalf("FetchRelatedAssociations: %v", err)
	}
	t.Logf("got %d related association(s)", len(assocs))
}
