// LEGACY tier: Siemplify external API (/api/external/v1) SEARCH surface —
// platform-wide case and entity search plus the filter-value helpers that drive
// the search UI. All are POST-with-body queries; results return RawJSON.
package legacy

import "context"

// CaseSearchEverything runs a full-text/structured search across cases. body is
// the freeform search request.
func (c *Client) CaseSearchEverything(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/CaseSearchEverything", body)
}

// EntitySearchEverything runs a search across entities. body is the freeform
// search request.
func (c *Client) EntitySearchEverything(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/EntitySearchEverything", body)
}

// GetCasesFilterValues returns the available case-search filter values.
func (c *Client) GetCasesFilterValues(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/GetCasesFilterValues", body)
}

// GetCasesFilterUserAndRoles returns the user/role filter values for case search.
func (c *Client) GetCasesFilterUserAndRoles(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/GetCasesFilterUserAndRoles", body)
}

// GetEntitiesFilterValues returns the available entity-search filter values.
func (c *Client) GetEntitiesFilterValues(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/GetEntitiesFilterValues", body)
}

// GetSearchResultsAsCsv runs a search and returns the results as CSV. body is the
// freeform search request.
func (c *Client) GetSearchResultsAsCsv(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/search/GetSearchResultsAsCsv", body)
}
