package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Case operations here target the Chronicle-host UUID cases collection on
// chronicle.googleapis.com (ADC). This is the ALTERNATE, currently-UNUSED path to
// a case: the first-class cases collection (get / list / patch / merge and the
// executeBulk* actions) HTTP-500s at every API version (v1/v1beta/v1alpha),
// server-side. The WORKING path for case operations is the SOAR host — see
// soar.ListCases (siemplify v1alpha) and the soar/legacy case verbs (the reliable
// AppKey path). There is one case reachable by several APIs, not several case
// systems; the SOAR-integer ⇄ SIEM-UUID link is exposed by legacy:legacyBatchGetCases
// (BatchGetCases in legacy.go), the one Chronicle-host case read that does answer.
//
// These methods are kept as the typed alternate-path surface (so an importer can
// still reach the collection if the host revives it) and to host the working
// batch-get bridge; do not reach for them as the case operating path.
//
// Every URL is built from the string project_id (numeric=false). caseURL rewrites
// the version segment per call (the client base is pinned to DefaultAPIVersion).
//
// DEVIATION: a SIEM case id is a UUID, but the underlying SOAR APIs historically
// keyed cases by integer id. The bulk/merge endpoints (executeBulk*, cases:merge)
// take the numeric id form in casesIds; get/list/patch take the resource-name /
// UUID form. We keep both as strings end-to-end so no precision is lost and the
// caller passes whatever the instance hands back.

// Case API version segments for the Chronicle-host cases collection. The
// first-class collection (caseAPIVersion) 500s at every version today — v1beta is
// only the path segment these alternate-path calls target, NOT a validated working
// pin; the working case path is on the SOAR host (soar.ListCases). caseAPIVersionLegacy
// carries the legacy: RPC reads (legacyBatchGetCases is the live SOAR⇄SIEM bridge;
// legacyListCases 404s). See docs/design/architecture.md §6.
const (
	caseAPIVersionLegacy = "v1alpha" // chronicle legacy: RPC reads (batchGet bridge works; listCases 404s)
	caseAPIVersion       = "v1beta"  // first-class cases collection path segment — 500s at every version (unused alternate)
)

// CasePriority is a case priority level. Values mirror the wrapper's
// CasePriority StrEnum (the wire form is the PRIORITY_* string).
type CasePriority string

const (
	CasePriorityUnspecified CasePriority = "PRIORITY_UNSPECIFIED"
	CasePriorityInfo        CasePriority = "PRIORITY_INFO"
	CasePriorityLow         CasePriority = "PRIORITY_LOW"
	CasePriorityMedium      CasePriority = "PRIORITY_MEDIUM"
	CasePriorityHigh        CasePriority = "PRIORITY_HIGH"
	CasePriorityCritical    CasePriority = "PRIORITY_CRITICAL"
)

// CaseCloseReason is a reason a case is closed. Values mirror the wrapper's
// CaseCloseReason StrEnum.
type CaseCloseReason string

const (
	CaseCloseReasonUnspecified  CaseCloseReason = "CLOSE_REASON_UNSPECIFIED"
	CaseCloseReasonMalicious    CaseCloseReason = "MALICIOUS"
	CaseCloseReasonNotMalicious CaseCloseReason = "NOT_MALICIOUS"
	CaseCloseReasonMaintenance  CaseCloseReason = "MAINTENANCE"
	CaseCloseReasonInconclusive CaseCloseReason = "INCONCLUSIVE"
	CaseCloseReasonUnknown      CaseCloseReason = "UNKNOWN"
)

// CaseStage is the investigation stage of a case. The wrapper's
// execute_bulk_change_stage takes a free-form string (no CaseStage enum exists
// in its models), so these constants reflect the standard SOAR stage names.
// Pass any string the instance accepts; these are the common defaults.
//
// DEVIATION: the wrapper has no CaseStage type — we define one for callers,
// while BulkChangeStage still accepts an arbitrary string.
type CaseStage string

const (
	CaseStageTriage        CaseStage = "Triage"
	CaseStageAssessment    CaseStage = "Assessment"
	CaseStageInvestigation CaseStage = "Investigation"
	CaseStageIncident      CaseStage = "Incident"
	CaseStageContainment   CaseStage = "Containment, Eradication, & Recovery"
	CaseStageImprovement   CaseStage = "Post-Incident Activity"
)

// Case is a single case. The API surface is broad and version-dependent, so the
// well-known scalar fields are typed and the rest is preserved verbatim in Raw
// for round-tripping. Name is the full resource name
// (projects/.../cases/{case}); Id is its trailing segment.
type Case struct {
	Name        string          `json:"name,omitempty"`
	Id          string          `json:"id,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	Priority    CasePriority    `json:"priority,omitempty"`
	Stage       string          `json:"stage,omitempty"`
	Status      string          `json:"status,omitempty"`
	CreateTime  string          `json:"createTime,omitempty"`
	UpdateTime  string          `json:"updateTime,omitempty"`
	Assignee    string          `json:"assignee,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Raw         json.RawMessage `json:"-"` // full server object, for fields not modeled above
}

// UnmarshalJSON keeps the typed fields and stashes the complete object in Raw so
// no server-provided field is lost on a round-trip.
func (c *Case) UnmarshalJSON(b []byte) error {
	type alias Case // avoid recursing into this method
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*c = Case(a)
	c.Raw = append(c.Raw[:0], b...)
	return nil
}

// CaseID returns the trailing segment of the case's resource Name, falling back
// to Id when Name is unset.
func (c *Case) CaseID() string {
	if c == nil {
		return ""
	}
	if c.Name != "" {
		return c.Name[strings.LastIndex(c.Name, "/")+1:]
	}
	return c.Id
}

// caseURL builds the absolute URL for a case endpoint at the given API version,
// rewriting the trailing version segment of the client's base URL. endpoint is
// appended after the instance path; an endpoint that begins with ":" is treated
// as an RPC-style method on the instance ({instance}:method), matching the
// wrapper. Use caseDo to issue the request.
func (c *Client) caseURL(version, endpoint string) string {
	base := c.baseURL
	if def := "/" + DefaultAPIVersion; strings.HasSuffix(base, def) {
		base = base[:len(base)-len(def)] + "/" + version
	}
	inst := c.instancePath(false)
	if strings.HasPrefix(endpoint, ":") {
		return base + "/" + inst + endpoint
	}
	return base + "/" + inst + "/" + strings.TrimLeft(endpoint, "/")
}

// caseDo issues an HTTP request to an absolute case URL, JSON-marshaling body
// (if non-nil) and decoding the response into out (if non-nil). It mirrors
// Client.do's behavior: non-2xx -> *APIError, transient 429/5xx and transport
// errors retried with capped exponential backoff, ctx honored.
//
// DEVIATION: Client.do hard-codes baseURL (pinned to DefaultAPIVersion), but
// cases span v1alpha (legacy reads) and v1beta (collection + bulk). Rather than
// papering over that with brittle "../" path tricks, caseDo accepts an already
// version-correct absolute URL (from caseURL) and reuses every other semantic of
// do unchanged.
func (c *Client) caseDo(ctx context.Context, method, absURL string, body, out any, opts ...requestOption) error {
	spec := &requestSpec{}
	for _, o := range opts {
		o(spec)
	}
	full := absURL
	if len(spec.query) > 0 {
		full += "?" + spec.query.Encode()
	}
	return c.doRequest(ctx, method, full, body, out)
}

// --- single-case reads (v1beta) ---------------------------------------------

// GetCase fetches a single case by id (UUID) or full resource name. expand
// (e.g. "tags,products") may be empty.
func (c *Client) GetCase(ctx context.Context, id, expand string) (*Case, error) {
	var opts []requestOption
	if expand != "" {
		opts = append(opts, withQuery(url.Values{"expand": {expand}}))
	}
	var cs Case
	if err := c.caseDo(ctx, http.MethodGet, c.caseURL(caseAPIVersion, "cases/"+caseResourceID(id)), nil, &cs, opts...); err != nil {
		return nil, err
	}
	return &cs, nil
}

// GetCases batch-fetches up to 1000 cases by id via the legacy batch endpoint
// (legacy:legacyBatchGetCases, v1alpha). ids may be UUIDs or full resource
// names; each is passed as a repeated "names" query param, mirroring the
// wrapper.
func (c *Client) GetCases(ctx context.Context, ids []string) ([]Case, error) {
	if len(ids) > 1000 {
		return nil, &APIError{
			Method: http.MethodGet,
			URL:    c.caseURL(caseAPIVersionLegacy, "legacy:legacyBatchGetCases"),
			Body:   fmt.Sprintf("maximum of 1000 cases can be retrieved in a batch, got %d", len(ids)),
		}
	}
	q := url.Values{}
	for _, id := range ids {
		q.Add("names", id)
	}
	var resp struct {
		Cases []Case `json:"cases"`
	}
	if err := c.caseDo(ctx, http.MethodGet, c.caseURL(caseAPIVersionLegacy, "legacy:legacyBatchGetCases"), nil, &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return resp.Cases, nil
}

// CaseSearch narrows a legacy case search (legacy:legacyListCases, v1alpha).
// Zero-value fields are omitted. A zero StartTime/EndTime means no bound.
type CaseSearch struct {
	StartTime        time.Time // filters on createTime.startTime
	EndTime          time.Time // filters on createTime.endTime
	CaseIDs          []string  // repeated caseId
	AssetIdentifiers []string  // repeated assetId
	TenantID         string
	PageSize         int    // defaults to 100 (wrapper default)
	PageToken        string // for manual pagination
}

// SearchCases runs the legacy case search and returns the raw JSON page (cases
// + pagination metadata are instance-shaped). It mirrors the wrapper's
// get_cases; callers decode the freeform page as needed.
//
// DEVIATION: the legacy response shape varies by instance, so we return the raw
// page rather than over-modeling it. Use ListCases for the typed, paginated
// v1beta collection.
func (c *Client) SearchCases(ctx context.Context, s CaseSearch) (json.RawMessage, error) {
	pageSize := s.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	q := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if s.PageToken != "" {
		q.Set("pageToken", s.PageToken)
	}
	if s.TenantID != "" {
		q.Set("tenantId", s.TenantID)
	}
	if !s.StartTime.IsZero() {
		q.Set("createTime.startTime", s.StartTime.UTC().Format("2006-01-02T15:04:05.000000Z"))
	}
	if !s.EndTime.IsZero() {
		q.Set("createTime.endTime", s.EndTime.UTC().Format("2006-01-02T15:04:05.000000Z"))
	}
	// DEVIATION: the wrapper assigns caseId/assetId into a plain dict in a loop,
	// so only the LAST id of each survives (a latent bug). We emit every id as a
	// repeated query param (caseId=a&caseId=b…), which is the correct contract
	// for a multi-id filter.
	for _, id := range s.CaseIDs {
		q.Add("caseId", id)
	}
	for _, a := range s.AssetIdentifiers {
		q.Add("assetId", a)
	}
	var raw json.RawMessage
	if err := c.caseDo(ctx, http.MethodGet, c.caseURL(caseAPIVersionLegacy, "legacy:legacyListCases"), nil, &raw, withQuery(q)); err != nil {
		return nil, err
	}
	return raw, nil
}

// --- list (v1beta, paginated) -----------------------------------------------

// CaseListOptions filters a ListCases call. Zero-value fields are omitted.
type CaseListOptions struct {
	Filter     string // filter expression
	OrderBy    string // comma-separated fields
	Expand     string // e.g. "tags,products"
	DistinctBy string
	PageSize   int // per-page cap (1-1000); 0 lets the server choose
}

// ListCases returns all cases in the v1beta collection, auto-paginating over
// nextPageToken (cap 50 pages). filter is a convenience for
// CaseListOptions.Filter; pageSize sets the per-page cap.
func (c *Client) ListCases(ctx context.Context, filter string, pageSize int) ([]Case, error) {
	return c.ListCasesOpts(ctx, CaseListOptions{Filter: filter, PageSize: pageSize})
}

// ListCasesOpts is ListCases with the full option set.
func (c *Client) ListCasesOpts(ctx context.Context, opt CaseListOptions) ([]Case, error) {
	var all []Case
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if opt.PageSize > 0 {
			q.Set("pageSize", strconv.Itoa(opt.PageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
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
		if opt.DistinctBy != "" {
			q.Set("distinctBy", opt.DistinctBy)
		}
		var resp struct {
			Cases         []Case `json:"cases"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.caseDo(ctx, http.MethodGet, c.caseURL(caseAPIVersion, "cases"), nil, &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Cases...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// --- patch (v1beta) ---------------------------------------------------------

// PatchCase partially updates a case (PATCH cases/{id}, v1beta). updates is the
// set of fields to change; updateMask (comma-separated field paths) scopes the
// update — pass "" to let the server infer it. The updated case is returned.
//
// DEVIATION: updates is a typed struct, so a "priority" sent as a string is the
// PRIORITY_* wire value by construction; the wrapper coerces a loose string to
// the enum at runtime. Mirror that by passing a CasePriority constant.
func (c *Client) PatchCase(ctx context.Context, id string, updates CaseUpdate, updateMask string) (*Case, error) {
	var opts []requestOption
	if updateMask != "" {
		opts = append(opts, withQuery(url.Values{"updateMask": {updateMask}}))
	}
	var cs Case
	if err := c.caseDo(ctx, http.MethodPatch, c.caseURL(caseAPIVersion, "cases/"+caseResourceID(id)), updates, &cs, opts...); err != nil {
		return nil, err
	}
	return &cs, nil
}

// CaseUpdate is the mutable subset of a case for PatchCase. Only set fields are
// sent; pair the set with a matching updateMask when the server needs one.
type CaseUpdate struct {
	DisplayName string       `json:"displayName,omitempty"`
	Priority    CasePriority `json:"priority,omitempty"`
	Stage       string       `json:"stage,omitempty"`
	Status      string       `json:"status,omitempty"`
	Assignee    string       `json:"assignee,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
}

// --- merge (v1beta) ---------------------------------------------------------

// MergeResult is the outcome of a MergeCases call.
type MergeResult struct {
	NewCaseID      string          `json:"newCaseId,omitempty"`
	IsRequestValid bool            `json:"isRequestValid,omitempty"`
	Errors         json.RawMessage `json:"errors,omitempty"`
}

// MergeCases merges sourceIDs into targetID (cases:merge, v1beta). Per the
// wrapper, the API expects ALL involved cases — sources plus the target — in
// casesIds, with the target also named separately in caseToMergeWith. We
// de-duplicate the union so the target is not listed twice.
func (c *Client) MergeCases(ctx context.Context, sourceIDs []string, targetID string) (*MergeResult, error) {
	seen := make(map[string]struct{}, len(sourceIDs)+1)
	all := make([]string, 0, len(sourceIDs)+1)
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		all = append(all, id)
	}
	for _, id := range sourceIDs {
		add(id)
	}
	add(targetID)

	body := struct {
		CasesIDs        []string `json:"casesIds"`
		CaseToMergeWith string   `json:"caseToMergeWith"`
	}{CasesIDs: all, CaseToMergeWith: targetID}

	var res MergeResult
	if err := c.caseDo(ctx, http.MethodPost, c.caseURL(caseAPIVersion, "cases:merge"), body, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// --- bulk actions (v1beta) --------------------------------------------------

// bulkAction posts a body to a cases:executeBulk* RPC method and discards the
// (empty) response, surfacing any *APIError.
func (c *Client) bulkAction(ctx context.Context, method string, body any) error {
	return c.caseDo(ctx, http.MethodPost, c.caseURL(caseAPIVersion, "cases:"+method), body, nil)
}

// BulkAddTag adds tags to multiple cases (cases:executeBulkAddTag).
func (c *Client) BulkAddTag(ctx context.Context, caseIDs, tags []string) error {
	body := struct {
		CasesIDs []string `json:"casesIds"`
		Tags     []string `json:"tags"`
	}{CasesIDs: caseIDs, Tags: tags}
	return c.bulkAction(ctx, "executeBulkAddTag", body)
}

// BulkAssign assigns multiple cases to a user (cases:executeBulkAssign).
func (c *Client) BulkAssign(ctx context.Context, caseIDs []string, username string) error {
	body := struct {
		CasesIDs []string `json:"casesIds"`
		UserName string   `json:"userName"`
	}{CasesIDs: caseIDs, UserName: username}
	return c.bulkAction(ctx, "executeBulkAssign", body)
}

// BulkChangePriority changes the priority of multiple cases
// (cases:executeBulkChangePriority).
func (c *Client) BulkChangePriority(ctx context.Context, caseIDs []string, priority CasePriority) error {
	body := struct {
		CasesIDs []string     `json:"casesIds"`
		Priority CasePriority `json:"priority"`
	}{CasesIDs: caseIDs, Priority: priority}
	return c.bulkAction(ctx, "executeBulkChangePriority", body)
}

// BulkChangeStage changes the stage of multiple cases
// (cases:executeBulkChangeStage). stage is free-form (see CaseStage).
func (c *Client) BulkChangeStage(ctx context.Context, caseIDs []string, stage string) error {
	body := struct {
		CasesIDs []string `json:"casesIds"`
		Stage    string   `json:"stage"`
	}{CasesIDs: caseIDs, Stage: stage}
	return c.bulkAction(ctx, "executeBulkChangeStage", body)
}

// CaseCloseOptions carries the optional fields of a bulk close. Zero values are
// omitted from the request.
type CaseCloseOptions struct {
	CloseComment      string            // optional comment recorded on close
	DynamicParameters []json.RawMessage // optional action-specific params (freeform)
}

// BulkClose closes multiple cases (cases:executeBulkClose). reason and rootCause
// are required by most close workflows; pass opt for the optional comment and
// dynamic parameters (nil for none).
func (c *Client) BulkClose(ctx context.Context, caseIDs []string, reason CaseCloseReason, rootCause string, opt *CaseCloseOptions) error {
	body := struct {
		CasesIDs          []string          `json:"casesIds"`
		CloseReason       CaseCloseReason   `json:"closeReason"`
		RootCause         string            `json:"rootCause,omitempty"`
		CloseComment      string            `json:"closeComment,omitempty"`
		DynamicParameters []json.RawMessage `json:"dynamicParameters,omitempty"`
	}{CasesIDs: caseIDs, CloseReason: reason, RootCause: rootCause}
	if opt != nil {
		body.CloseComment = opt.CloseComment
		body.DynamicParameters = opt.DynamicParameters
	}
	return c.bulkAction(ctx, "executeBulkClose", body)
}

// BulkReopen reopens multiple cases (cases:executeBulkReopen). reopenComment is
// recorded on the reopen action.
func (c *Client) BulkReopen(ctx context.Context, caseIDs []string, reopenComment string) error {
	body := struct {
		CasesIDs      []string `json:"casesIds"`
		ReopenComment string   `json:"reopenComment"`
	}{CasesIDs: caseIDs, ReopenComment: reopenComment}
	return c.bulkAction(ctx, "executeBulkReopen", body)
}

// caseResourceID reduces a full case resource name to its trailing id segment,
// mirroring the wrapper's format_resource_id: a value starting with "projects/"
// is reduced to its last "/"-segment; anything else is returned unchanged.
func caseResourceID(id string) string {
	if strings.HasPrefix(id, "projects/") {
		return id[strings.LastIndex(id, "/")+1:]
	}
	return id
}

// CountCasePriorities calls the chronicle-host cases:countPriorities RPC
// (e.g. filter "status=OPEN") — the twin of the SOAR-host method. The RPC is
// not served on current deployments; the CLI's `soar case counts` derives the
// same numbers from the cases list's totalSize instead (CountCasesByPriority
// in the soar package). Kept for SDK importers on instances that serve it.
func (c *Client) CountCasePriorities(ctx context.Context, filter string) (json.RawMessage, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, fmt.Errorf("chronicle: filter is required")
	}
	q := url.Values{"filter": {filter}}
	var out json.RawMessage
	if err := c.get(ctx, c.resourcePath("cases:countPriorities", false), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}
