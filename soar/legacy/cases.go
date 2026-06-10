// LEGACY tier: the Siemplify external API (/api/external/v1) case surface.
//
// This is the broad, reliable case path — the SOAR case verbs and bulk operations
// run on it, and it is the fallback when a modern v1alpha case call 500s. It stays
// importable; it is not slated for removal. Every method here speaks the
// offset-style external API via c.t.External and returns json.RawMessage for the
// deeply-nested, schema-unstable case payloads (the caller decodes only the fields
// it needs).
//
// DUAL CASE-ID GOTCHA: SOAR exposes two unrelated case identifiers. The modern
// v1alpha surface uses the *alert/case resource name* (a string), while this
// legacy surface keys off the *SOAR integer case id* (the numeric id shown in
// the Siemplify case wall URL). Every id parameter and every field below
// (casesIds, caseID) is the LEGACY INTEGER id — do NOT pass a v1alpha resource
// name or alert group identifier here; they are not interchangeable.
package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// CaseStatus values for CaseQueueRequest.Statuses.
const (
	CaseStatusOpen   = 1 // OPEN
	CaseStatusClosed = 2 // CLOSED
)

// CaseQueueRequest filters and pages the legacy case-queue (case cards) view.
// SortBy and the per-card payloads are freeform legacy shapes, so SortBy is
// left as []any. Pagination is offset-style: RequestedPage is 0-based and
// PageSize caps the cards returned.
type CaseQueueRequest struct {
	SortBy        []any `json:"sortBy"`
	RequestedPage int   `json:"requestedPage"`
	PageSize      int   `json:"pageSize"`
	Statuses      []int `json:"statuses"` // 1=OPEN 2=CLOSED
}

// CloseReason is the legacy bulk-close reason code (the server's CloseReasonEnum).
type CloseReason int

// CloseReason values accepted by BulkCloseCases. The integer encoding is the
// server's, not alphabetical — Malicious is 0, per the swagger CloseReasonEnum
// description (Malicious=0 / NotMalicious=1 / Maintenance=2 / Inconclusive=3 /
// Unknown=4). Do not reorder.
const (
	CloseMalicious    CloseReason = 0
	CloseNotMalicious CloseReason = 1
	CloseMaintenance  CloseReason = 2
	CloseInconclusive CloseReason = 3
	CloseUnknown      CloseReason = 4
)

// String returns the reason name (for CLI/log output).
func (r CloseReason) String() string {
	switch r {
	case CloseMalicious:
		return "Malicious"
	case CloseNotMalicious:
		return "NotMalicious"
	case CloseMaintenance:
		return "Maintenance"
	case CloseInconclusive:
		return "Inconclusive"
	case CloseUnknown:
		return "Unknown"
	default:
		return "CloseReason(" + strconv.Itoa(int(r)) + ")"
	}
}

// CasePriority is the case priority scale (the server's CasePriority enum). The
// integer values are non-contiguous (sourced from the swagger schema description).
type CasePriority int

// CasePriority values for ManualCaseRequest.Priority and the priority verbs.
const (
	PriorityInformative CasePriority = -1
	PriorityUnchanged   CasePriority = 0
	PriorityLow         CasePriority = 40
	PriorityMedium      CasePriority = 60
	PriorityHigh        CasePriority = 80
	PriorityCritical    CasePriority = 100
)

// String returns the priority's label ("Low" … "Critical"), or the raw number
// for an unmapped value.
func (p CasePriority) String() string {
	switch p {
	case PriorityInformative:
		return "Informative"
	case PriorityUnchanged:
		return "Unchanged"
	case PriorityLow:
		return "Low"
	case PriorityMedium:
		return "Medium"
	case PriorityHigh:
		return "High"
	case PriorityCritical:
		return "Critical"
	}
	return "CasePriority(" + strconv.Itoa(int(p)) + ")"
}

// ParseCasePriority maps a priority name (or one of the server's raw integer
// codings) to the typed CasePriority. Only the server's defined, settable values
// are accepted — the integer scale is non-contiguous (Low=40 … Critical=100,
// Informative=-1), so an arbitrary number is rejected rather than passed through
// as an undefined priority (and 0/"Unchanged" is rejected as a silent no-op).
func ParseCasePriority(s string) (CasePriority, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "informative", "info":
		return PriorityInformative, nil
	case "low":
		return PriorityLow, nil
	case "medium":
		return PriorityMedium, nil
	case "high":
		return PriorityHigh, nil
	case "critical":
		return PriorityCritical, nil
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		switch p := CasePriority(n); p {
		case PriorityInformative, PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical:
			return p, nil
		}
	}
	return 0, fmt.Errorf("legacy: invalid case priority %q (use informative|low|medium|high|critical)", s)
}

// BulkCloseRequest closes one or more cases in a single legacy operation.
// CasesIDs are SOAR INTEGER case ids (see the dual case-id gotcha above).
// DynamicParameters is a freeform legacy bag, usually empty ([]any{}).
type BulkCloseRequest struct {
	CasesIDs          []int       `json:"casesIds"`
	CloseReason       CloseReason `json:"closeReason"`
	RootCause         string      `json:"rootCause"`
	CloseComment      string      `json:"closeComment"`
	DynamicParameters []any       `json:"dynamicParameters"`
}

// ListCaseCards returns the case-queue cards matching req. The response is the
// raw legacy page payload (cards plus a total count); decode the slice of cards
// the caller needs. Page with req.RequestedPage / req.PageSize.
func (c *Client) ListCaseCards(ctx context.Context, req CaseQueueRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodPost, "/cases-queue/GetCaseCardsByRequest", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ManualCaseRequest is the body for CreateManualCase (an analyst-authored case).
//
// The collection fields (Entities, Playbooks, Tags) are ALWAYS serialized — their
// json tags carry no omitempty and CreateManualCase normalizes nil to empty —
// because the legacy server does not null-guard them: omitting any one makes the
// call throw a server-side 500 *after* it has already created the case (leaving an
// orphan). AssignedUser must be non-empty: a username, or a role as "@RoleName".
// OccurenceTime is an RFC3339 timestamp. Priority is a CasePriority enum value
// (0/40/60/80/100, or -1 for Informative). SLAExpirationDateTime is optional.
type ManualCaseRequest struct {
	Title                       string       `json:"title"`
	AssignedUser                string       `json:"assignedUser"`
	Reason                      string       `json:"reason"`
	Priority                    CasePriority `json:"priority"`
	Environment                 string       `json:"environment"`
	IsImportant                 bool         `json:"isImportant"`
	AlertName                   string       `json:"alertName"`
	OccurenceTime               string       `json:"occurenceTime"`
	SLAExpirationDateTime       *string      `json:"slaExpirationDateTime"`
	Entities                    []any        `json:"entities"`
	Playbooks                   []string     `json:"playbooks"`
	AutomaticPlaybookAttachment bool         `json:"automaticPlaybookAttachment"`
	Tags                        []string     `json:"tags"`
}

// CreateManualCase creates a manual (analyst-authored) case and returns the new
// SOAR integer case id. It forces the Entities/Playbooks/Tags collections to
// non-null so the server's missing null-guards cannot 500 after creating the case.
//
// DEVIATION: unlike the freeform single-case actions in cases_actions.go, this is
// typed — the empty-collection contract is load-bearing (a null trips a server
// NPE), so the request is modeled rather than left freeform.
func (c *Client) CreateManualCase(ctx context.Context, req ManualCaseRequest) (int, error) {
	if req.Entities == nil {
		req.Entities = []any{}
	}
	if req.Playbooks == nil {
		req.Playbooks = []string{}
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	var id int
	if err := c.t.External(ctx, http.MethodPost, "/cases/CreateManualCase", req, &id); err != nil {
		return 0, err
	}
	return id, nil
}

// BulkCloseCases closes every case in req.CasesIDs with one operation. This is a
// live mutation against production cases — confirm the id set first.
func (c *Client) BulkCloseCases(ctx context.Context, req BulkCloseRequest) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodPost, "/cases-queue/bulk-operations/ExecuteBulkCloseCase", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCaseFullDetails returns the full legacy detail blob for one case. caseID is
// the SOAR INTEGER case id (see the dual case-id gotcha above). The shape is
// large and unstable, so it is returned raw.
func (c *Client) GetCaseFullDetails(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	// DEVIATION: the legacy route is path-positional ("/GetCaseFullDetails/<id>"),
	// not a query param; build it explicitly rather than via Query opts.
	path := "/cases/GetCaseFullDetails/" + strconv.Itoa(caseID)
	if err := c.t.External(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddCaseComment posts a comment to a case. body is the freeform legacy payload
// (typically {caseId:<int>, comment:"…"}); it carries the SOAR INTEGER case id.
func (c *Client) AddCaseComment(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodPost, "/cases/AddCaseComment", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddCaseTag tags a case. body is the freeform legacy payload (typically
// {caseId:<int>, tag:"…"}); it carries the SOAR INTEGER case id.
func (c *Client) AddCaseTag(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodPost, "/cases/AddCaseTag", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ChangeCasePriority changes a case's priority. body is the freeform legacy
// payload (typically {caseId:<int>, priority:<int>}); it carries the SOAR
// INTEGER case id.
func (c *Client) ChangeCasePriority(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, http.MethodPost, "/cases/ChangeCasePriority", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
