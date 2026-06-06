package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Reference-list view enums (REFERENCE_LIST_VIEW_*). FULL is the only one this
// SDK uses on list — it populates entries (content lines) and associated rule
// counts, which BASIC omits.
const (
	refListViewBasic = "REFERENCE_LIST_VIEW_BASIC"
	refListViewFull  = "REFERENCE_LIST_VIEW_FULL"
)

// ReferenceListEntry is one value (content line) of a reference list.
type ReferenceListEntry struct {
	Value string `json:"value"`
}

// ReferenceList is a Chronicle reference list — a named set of values (plain
// strings, regexes, or CIDRs per SyntaxType) referenced from YARA-L rules.
//
// SyntaxType is one of REFERENCE_LIST_SYNTAX_TYPE_PLAIN_TEXT_STRING /
// _REGEX / _CIDR. ScopeInfo is left as raw JSON: it is a freeform, rarely-set
// nested object whose shape callers don't need to branch on.
type ReferenceList struct {
	Name               string               `json:"name"`
	DisplayName        string               `json:"displayName"`
	Description        string               `json:"description"`
	SyntaxType         string               `json:"syntaxType"`
	Entries            []ReferenceListEntry `json:"entries"`
	ScopeInfo          json.RawMessage      `json:"scopeInfo,omitempty"`
	RevisionCreateTime string               `json:"revisionCreateTime"`
}

// ListReferenceLists returns every reference list in the instance using the
// FULL view, so Entries are populated. Reference lists use the project ID form
// (numeric=false). Results are returned in ascending alphabetical order by name
// (server-side ordering).
//
// DEVIATION: the wrapper defaults list to the BASIC view (no entries); we force
// FULL so the mirror layer can snapshot entries without a second GetReferenceList
// round-trip per list.
func (c *Client) ListReferenceLists(ctx context.Context) ([]ReferenceList, error) {
	var all []ReferenceList
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{
			"pageSize": {"1000"},
			"view":     {refListViewFull},
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			ReferenceLists []ReferenceList `json:"referenceLists"`
			NextPageToken  string          `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("referenceLists", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		for i := range resp.ReferenceLists {
			resp.ReferenceLists[i].Name = c.canonicalRefListName(resp.ReferenceLists[i].Name)
		}
		all = append(all, resp.ReferenceLists...)
		return resp.NextPageToken, nil
	})
	return all, err
}
