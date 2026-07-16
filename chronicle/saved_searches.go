package chronicle

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// saved_searches.go is the server-side "Search Manager" saved & shared searches
// (projects.locations.instances.users.searchQueries) — the shareable, server-stored
// queries the console lists, distinct from the tool's local saved-query pack.
// Chronicle host, v1alpha, project ID form, per-user under users/me.

const savedSearchUser = "me"

// SharingMode is EntryMetadata.sharingMode — private vs shared with the whole org.
type SharingMode string

const (
	SharingModePrivate            SharingMode = "MODE_PRIVATE"
	SharingModeSharedWithCustomer SharingMode = "MODE_SHARED_WITH_CUSTOMER"
)

// QueryType is the SearchQuery.queryType enum (QUERY_TYPE_* tokens).
type QueryType string

const (
	QueryTypeUnspecified QueryType = "QUERY_TYPE_UNSPECIFIED"
	QueryTypeUDM         QueryType = "QUERY_TYPE_UDM_QUERY"
	QueryTypeRawLog      QueryType = "QUERY_TYPE_RAW_LOG_QUERY"
	QueryTypeStats       QueryType = "QUERY_TYPE_STATS_QUERY"
	QueryTypeDashboard   QueryType = "QUERY_TYPE_DASHBOARD_QUERY"
)

// QueryLanguage is the SearchQuery.queryLanguage enum.
type QueryLanguage string

const (
	QueryLanguageUnspecified QueryLanguage = "QUERY_LANGUAGE_UNSPECIFIED"
	QueryLanguageYL2         QueryLanguage = "QUERY_LANGUAGE_YL2"
	QueryLanguageSQL         QueryLanguage = "QUERY_LANGUAGE_SQL"
)

// SavedSearch is the v1alpha SearchQuery resource. Name/QueryID/UserID and the
// metadata timestamps are server-managed (output only).
type SavedSearch struct {
	Name            string           `json:"name,omitempty"`
	Metadata        *SavedSearchMeta `json:"metadata,omitempty"`
	DisplayName     string           `json:"displayName,omitempty"`
	Query           string           `json:"query,omitempty"`
	QueryID         string           `json:"queryId,omitempty"`
	UserID          string           `json:"userId,omitempty"`
	Description     string           `json:"description,omitempty"`
	QueryType       QueryType        `json:"queryType,omitempty"`
	CaseInsensitive bool             `json:"caseInsensitive"`
	QueryLanguage   QueryLanguage    `json:"queryLanguage,omitempty"`
}

// SavedSearchMeta is EntryMetadata. An absent SharingMode means private.
type SavedSearchMeta struct {
	SharingMode SharingMode `json:"sharingMode,omitempty"`
	CreateTime  string      `json:"createTime,omitempty"`
	UpdateTime  string      `json:"updateTime,omitempty"`
}

// Shared reports whether the saved search is shared with the whole customer/org.
func (s *SavedSearch) Shared() bool {
	return s.Metadata != nil && s.Metadata.SharingMode == SharingModeSharedWithCustomer
}

type listSavedSearchesResponse struct {
	SearchQueries []SavedSearch `json:"searchQueries"`
	NextPageToken string        `json:"nextPageToken"`
}

// ListSavedSearches returns the current user's saved searches plus any shared with
// the whole customer/org by other users.
//
// Endpoint: GET {instance}/users/me/searchQueries?pageSize=1000.
func (c *Client) ListSavedSearches(ctx context.Context) ([]SavedSearch, error) {
	var all []SavedSearch
	err := paginate(50, func(pageToken string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var resp listSavedSearchesResponse
		if err := c.get(ctx, c.savedSearchesPath(), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.SearchQueries...)
		return resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetSavedSearch fetches one saved search by query id (UUID) or full resource name.
func (c *Client) GetSavedSearch(ctx context.Context, idOrName string) (*SavedSearch, error) {
	var s SavedSearch
	if err := c.get(ctx, c.savedSearchName(idOrName), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSavedSearch creates a saved search. Query is required; QueryType defaults
// to QUERY_TYPE_UDM_QUERY and QueryLanguage to QUERY_LANGUAGE_YL2 when unset. The
// client-supplied id (searchQueryId) is generated here. Set in.Metadata.SharingMode
// to share at create time, or call ShareSavedSearch afterwards.
//
// Endpoint: POST {instance}/users/me/searchQueries?searchQueryId={uuid}.
func (c *Client) CreateSavedSearch(ctx context.Context, in SavedSearch) (*SavedSearch, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, fmt.Errorf("chronicle: CreateSavedSearch requires a non-empty query")
	}
	if in.QueryType == "" {
		in.QueryType = QueryTypeUDM
	}
	if in.QueryLanguage == "" {
		in.QueryLanguage = QueryLanguageYL2
	}
	// Output-only fields must not be sent on create.
	in.Name, in.QueryID, in.UserID = "", "", ""
	q := url.Values{"searchQueryId": {newUUID()}}
	var out SavedSearch
	if err := c.post(ctx, c.savedSearchesPath(), &in, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSavedSearch patches the named fields of a saved search. fields is the
// FieldMask (dotted for nested, e.g. "metadata.sharingMode", "displayName",
// "query", "description", "caseInsensitive"); at least one is required.
//
// Endpoint: PATCH {instance}/users/me/searchQueries/{id}?updateMask={fields}.
func (c *Client) UpdateSavedSearch(ctx context.Context, idOrName string, in SavedSearch, fields ...string) (*SavedSearch, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("chronicle: UpdateSavedSearch requires an updateMask (fields to change)")
	}
	in.Name, in.QueryID, in.UserID = "", "", ""
	q := url.Values{"updateMask": {strings.Join(fields, ",")}}
	var out SavedSearch
	if err := c.patch(ctx, c.savedSearchName(idOrName), &in, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// ShareSavedSearch toggles a saved search's sharing (metadata.sharingMode) — share
// with the whole customer/org, or revert to private.
func (c *Client) ShareSavedSearch(ctx context.Context, idOrName string, mode SharingMode) (*SavedSearch, error) {
	return c.UpdateSavedSearch(ctx, idOrName,
		SavedSearch{Metadata: &SavedSearchMeta{SharingMode: mode}}, "metadata.sharingMode")
}

// DeleteSavedSearch deletes a saved search by id or full resource name.
func (c *Client) DeleteSavedSearch(ctx context.Context, idOrName string) error {
	return c.do(ctx, http.MethodDelete, c.savedSearchName(idOrName), nil, nil)
}

// savedSearchesPath is the per-user searchQueries collection path.
func (c *Client) savedSearchesPath() string {
	return c.resourcePath("users/"+savedSearchUser+"/searchQueries", false)
}

// savedSearchName resolves a bare query id to the full resource name; a value that
// already looks like a resource path (contains "/") is used as-is.
func (c *Client) savedSearchName(idOrName string) string {
	if strings.Contains(idOrName, "/") {
		return idOrName
	}
	return c.resourcePath("users/"+savedSearchUser+"/searchQueries/"+idOrName, false)
}
