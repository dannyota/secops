package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

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
