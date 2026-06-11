package soar

// Tier: MODERN — v1alpha case listing.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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
	Title     string          `json:"-"` // tolerant: displayName/title
	Assignee  string          `json:"-"` // tolerant: assignee/assignedUser/owner
	Raw       json.RawMessage `json:"-"` // full case object as returned
}

// UnmarshalJSON fills the stable typed fields, keeps the full object in Raw, and
// resolves Title/Assignee tolerantly — the v1alpha case schema has used different
// keys for those across revisions (and the existing "cases"/"items" envelope split
// shows the surface still moves), so we pick the first present key rather than pin
// one that a future revision renames.
func (c *Case) UnmarshalJSON(data []byte) error {
	type alias Case // alias drops this method, so the inner Unmarshal won't recurse
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = Case(a)
	c.Raw = append(json.RawMessage(nil), data...)
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) == nil {
		c.Title = firstStringField(m, "displayName", "title")
		c.Assignee = firstStringField(m, "assignee", "assignedUser", "assigneeUsername", "owner")
	}
	return nil
}

// firstStringField returns the first key in keys whose value decodes to a
// non-empty string. Used for schema-tolerant field resolution.
func firstStringField(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

// listCasesPage is the Google-style list envelope. The v1alpha surface has used
// both "cases" and the generic "items" key across revisions, so we accept either.
// TotalSize is the FULL filtered count, present on every page regardless of
// pageSize — the basis of the cheap exact counts (CountCases).
type listCasesPage struct {
	Cases         []json.RawMessage `json:"cases"`
	Items         []json.RawMessage `json:"items"`
	TotalSize     json.Number       `json:"totalSize"`
	NextPageToken string            `json:"nextPageToken"`
}

func (p *listCasesPage) records() []json.RawMessage {
	if len(p.Cases) > 0 {
		return p.Cases
	}
	return p.Items
}

// CaseListOptions tunes ListCasesOpts; all fields are optional and map to the
// v1alpha cases list query parameters (the same the SecOps web UI sends).
type CaseListOptions struct {
	PageSize int    // per-request page cap (<=0 lets the server choose)
	Filter   string // server-side filter, e.g. "status = 'OPENED'"
	OrderBy  string // sort, e.g. "updateTime desc"
	Expand   string // comma-separated fields to inline, e.g. "products,tasks,tags,closureDetails,sla,alertsSla"
	MaxItems int    // stop once this many records are collected (<=0 = all pages)
}

// ListCases returns every case as a raw JSON object (pageSize bounds each
// request; <=0 lets the server choose). It is a thin wrapper over ListCasesOpts.
func (c *Client) ListCases(ctx context.Context, pageSize int) ([]json.RawMessage, error) {
	return c.ListCasesOpts(ctx, CaseListOptions{PageSize: pageSize})
}

// ListCasesTyped is ListCasesOpts decoded into the typed Case view (id ·
// displayName · status · priority · stage · assignee, plus Raw), so every consumer
// gets the same minimal struct instead of redefining its own over the raw JSON.
func (c *Client) ListCasesTyped(ctx context.Context, opt CaseListOptions) ([]Case, error) {
	raws, err := c.ListCasesOpts(ctx, opt)
	if err != nil {
		return nil, err
	}
	out := make([]Case, 0, len(raws))
	for _, r := range raws {
		var cs Case
		if err := json.Unmarshal(r, &cs); err != nil {
			continue // skip an unparseable record rather than fail the whole list
		}
		out = append(out, cs)
	}
	return out, nil
}

// ListCasesOpts returns cases as raw JSON, paging through the v1alpha
// {cases|items, nextPageToken} response, applying server-side filter/orderBy and
// optional field expansion. Pagination stops at opt.MaxItems (when set) and is
// otherwise capped by the runaway backstop (listMaxPages).
//
// DEVIATION: raw case JSON is returned because the v1alpha case schema is large
// and still moving; typed accessors (see Case) can layer on later.
func (c *Client) ListCasesOpts(ctx context.Context, opt CaseListOptions) ([]json.RawMessage, error) {
	var out []json.RawMessage

	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{}
		if opt.PageSize > 0 {
			q.Set("pageSize", strconv.Itoa(opt.PageSize))
		}
		if opt.Filter != "" {
			q.Set("filter", opt.Filter)
		}
		if opt.OrderBy != "" {
			q.Set("orderBy", opt.OrderBy)
		}
		if opt.Expand != "" {
			q.Set("expand", opt.Expand)
		}
		if token != "" {
			q.Set("pageToken", token)
		}

		var page listCasesPage
		if err := c.t.V1Alpha(ctx, "GET", "cases", nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		out = append(out, page.records()...)
		// Stop paging once enough records are collected — a bounded list (e.g. the
		// --limit-capped `soar case list`) must not paginate the whole tenant.
		if opt.MaxItems > 0 && len(out) >= opt.MaxItems {
			return "", nil
		}
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	if opt.MaxItems > 0 && len(out) > opt.MaxItems {
		out = out[:opt.MaxItems]
	}
	return out, nil
}

// CountCases returns the exact number of cases matching the server-side
// filter without fetching them: every page of the modern cases list carries
// totalSize, so a single pageSize=1 request is a cheap exact count. A
// zero-match query answers HTTP 204 with an empty body, which counts as 0.
func (c *Client) CountCases(ctx context.Context, filter string) (int, error) {
	q := url.Values{"pageSize": {"1"}}
	if strings.TrimSpace(filter) != "" {
		q.Set("filter", filter)
	}
	var page listCasesPage
	if err := c.t.V1Alpha(ctx, "GET", "cases", nil, &page, transport.Query(q)); err != nil {
		return 0, err
	}
	if page.TotalSize == "" {
		return 0, nil
	}
	n, err := page.TotalSize.Int64()
	if err != nil {
		return 0, fmt.Errorf("soar: totalSize %q: %w", page.TotalSize, err)
	}
	return int(n), nil
}

// CasePriorityTokens are the modern v1alpha case priority filter tokens,
// lowest first — the values `priority = '<token>'` accepts.
var CasePriorityTokens = []string{
	"PRIORITY_INFO", "PRIORITY_LOW", "PRIORITY_MEDIUM", "PRIORITY_HIGH", "PRIORITY_CRITICAL",
}

// CountCasesByPriority returns per-priority case counts for an optional base
// filter: one cheap CountCases per priority token, composed as
// "(<base>) and (priority = '<token>')". The queue's per-priority numbers in
// one call — the cases:countPriorities RPC is not served (the web UI builds
// its queue from filtered lists), so counts come from totalSize instead.
func (c *Client) CountCasesByPriority(ctx context.Context, baseFilter string) (map[string]int, error) {
	out := make(map[string]int, len(CasePriorityTokens))
	for _, tok := range CasePriorityTokens {
		f := "priority = '" + tok + "'"
		if strings.TrimSpace(baseFilter) != "" {
			f = "(" + baseFilter + ") and (" + f + ")"
		}
		n, err := c.CountCases(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("soar: count %s: %w", tok, err)
		}
		out[tok] = n
	}
	return out, nil
}

// GetCustomCases returns all custom (simulated) case names on the instance.
func (c *Client) GetCustomCases(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacyCases:getCustomCases", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCustomCaseDetails returns the config of one custom simulation.
func (c *Client) GetCustomCaseDetails(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:getCustomCaseDetails", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateSimulatedCustomCase creates a custom test case from alert/event field specs.
func (c *Client) CreateSimulatedCustomCase(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:createSimulatedCustomCase", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GenerateUseCases generates test cases from templates or custom names.
func (c *Client) GenerateUseCases(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:generateUseCases", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SimulateAlert simulates an alert inside a case for playbook testing.
func (c *Client) SimulateAlert(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:simulateAlert", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteUseCase deletes a custom simulation.
func (c *Client) DeleteUseCase(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:deleteUseCase", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExecuteManualAction runs a single integration action against a case via the
// v1alpha SOAR-host legacyCases:executeManualAction surface. body carries the
// action selectors (caseId, actionProvider, actionName, alertGroupIdentifiers,
// properties incl. ScriptName/IntegrationInstance/ScriptParametersEntityFields).
// Returns the full action result (resultCode, message, resultJsonObject, …).
func (c *Client) ExecuteManualAction(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyCases:executeManualAction", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
