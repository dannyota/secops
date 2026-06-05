package legacy

import (
	"testing"
)

// TestLiveSearchReads exercises the SEARCH filter-value endpoints (safe reads).
// Runs under SECOPS_SOAR_SMOKE=1.
//
// Every method in the search tag is POST-with-body, but the three filter-value
// endpoints take a small, well-defined request — typeOfFilter (an integer enum),
// numberOfValuesToReturn, searchTerm — and return the values available for that
// filter on the tenant. They need no prior setup, so a minimal body makes them
// reliable reads. The freeform CaseSearchEverything / EntitySearchEverything /
// GetSearchResultsAsCsv searches need a query shape we cannot safely construct
// here, so they are intentionally excluded.
func TestLiveSearchReads(t *testing.T) {
	lc, ctx := liveClient(t)

	// Minimal case-filter request: enum 0 == Tags (per swagger
	// CaseSearchFilterTypeEnum); empty searchTerm returns the top values.
	casesBody := map[string]any{
		"typeOfFilter":           0, // Tags
		"numberOfValuesToReturn": 10,
		"searchTerm":             "",
	}
	// UsersAndSocRoles == 11 for the user/role filter endpoint.
	usersBody := map[string]any{
		"typeOfFilter":           11, // UsersAndSocRoles
		"numberOfValuesToReturn": 10,
		"searchTerm":             "",
	}
	// Minimal entity-filter request: enum 0 == OperationSystem (per swagger
	// EntitySearchFilterTypeEnum).
	entitiesBody := map[string]any{
		"typeOfFilter":           0, // OperationSystem
		"numberOfValuesToReturn": 10,
		"searchTerm":             "",
	}

	readProbe(t, "search/GetCasesFilterValues", func() (RawJSON, error) {
		return lc.GetCasesFilterValues(ctx, casesBody)
	})
	readProbe(t, "search/GetCasesFilterUserAndRoles", func() (RawJSON, error) {
		return lc.GetCasesFilterUserAndRoles(ctx, usersBody)
	})
	readProbe(t, "search/GetEntitiesFilterValues", func() (RawJSON, error) {
		return lc.GetEntitiesFilterValues(ctx, entitiesBody)
	})
}
