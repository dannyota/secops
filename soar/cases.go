package soar

// Tier: MODERN — v1alpha case listing.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"danny.vn/secops/soar/internal/transport"
)

// Case is a minimal typed envelope over a v1alpha SOAR case. Only the few stable
// top-level fields are surfaced; the full, large, and still-evolving case schema
// is preserved verbatim in Raw.
//
// DEVIATION: callers that want the complete object should read Raw; we
// deliberately do not model the entire v1alpha case schema here.
type Case struct {
	Name      string          `json:"name"`      // resource name (…/cases/<id>)
	DisplayID string          `json:"displayId"` // human-facing case number
	Priority  string          `json:"priority"`
	Stage     string          `json:"stage"`
	Status    string          `json:"status"`
	Raw       json.RawMessage `json:"-"` // full case object as returned
}

// listCasesPage is the Google-style list envelope. The v1alpha surface has used
// both "cases" and the generic "items" key across revisions, so we accept either.
type listCasesPage struct {
	Cases         []json.RawMessage `json:"cases"`
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
}

func (p *listCasesPage) records() []json.RawMessage {
	if len(p.Cases) > 0 {
		return p.Cases
	}
	return p.Items
}

// ListCases returns every case as a raw JSON object, paging through the v1alpha
// {cases|items, nextPageToken} response. pageSize bounds each request (a
// non-positive value lets the server pick its default). Pagination is capped at
// 50 pages.
//
// DEVIATION: raw case JSON is returned because the v1alpha case schema is large
// and still moving; typed accessors (see Case) can layer on later.
func (c *Client) ListCases(ctx context.Context, pageSize int) ([]json.RawMessage, error) {
	var out []json.RawMessage

	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}

		var page listCasesPage
		if err := c.t.V1Alpha(ctx, "GET", "cases", nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		out = append(out, page.records()...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
