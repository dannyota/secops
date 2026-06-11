package soar

// Tier: MODERN — v1alpha AI-assist reads on the cases collection (SOAR host).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// CaseSummary is the structured Gemini summary of a case from
// cases:getOrCreateCaseSummary — Google's own AI pre-digest of the case
// (reasons, next steps, narrative). Generation is asynchronous: State runs
// PENDING_START → IN_PROGRESS → SUCCESSFUL | ERROR; callers poll with
// isFirstRequest=false until it settles.
type CaseSummary struct {
	Reasons   []string `json:"reasons"`
	NextSteps []string `json:"nextSteps"`
	Summary   string   `json:"summary"`
	State     string   `json:"state"`

	// Raw is the complete response (markdownResults, updateTime, …).
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed fields and keeps the full payload.
func (s *CaseSummary) UnmarshalJSON(b []byte) error {
	type alias CaseSummary
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*s = CaseSummary(a)
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// CaseSummary generation states.
const (
	SummaryStateSuccessful = "SUCCESSFUL"
	SummaryStateError      = "ERROR"
	SummaryStateInProgress = "IN_PROGRESS"
	SummaryStatePending    = "PENDING_START"
)

// Settled reports whether generation has finished (successfully or not).
func (s *CaseSummary) Settled() bool {
	return s != nil && (s.State == SummaryStateSuccessful || s.State == SummaryStateError)
}

// GetOrCreateCaseSummary fetches (generating on first request) the AI summary
// of one case. caseID is the case's modern resource id (the SOAR integer id as
// a string). Set isFirstRequest on the first call; poll with false until
// Settled.
func (c *Client) GetOrCreateCaseSummary(ctx context.Context, caseID string, isFirstRequest bool) (*CaseSummary, error) {
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("soar: caseID is required")
	}
	body := map[string]bool{"isFirstRequest": isFirstRequest}
	var out CaseSummary
	if err := c.t.V1Alpha(ctx, "POST", "cases/"+url.PathEscape(caseID)+":getOrCreateCaseSummary", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CountCasePriorities calls the cases:countPriorities RPC. The RPC is not
// served on current deployments (404; the web UI builds its queue numbers
// from filtered lists instead) — use CountCasesByPriority, which derives the
// same counts from the list's totalSize. Kept for instances that may serve
// the RPC.
func (c *Client) CountCasePriorities(ctx context.Context, filter string) (json.RawMessage, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, fmt.Errorf("soar: filter is required")
	}
	var out json.RawMessage
	q := url.Values{"filter": {filter}}
	if err := c.t.V1Alpha(ctx, "GET", "cases:countPriorities", nil, &out, transport.Query(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// CaseAlert is one entry of a case's modern caseAlerts sub-collection. ID is
// the NUMERIC caseAlert id the AI-recommendation verbs key on; Identifier and
// AlertGroupIdentifier are the string forms the legacy lane uses.
type CaseAlert struct {
	ID                   json.Number     `json:"id"`
	Identifier           string          `json:"identifier"`
	AlertGroupIdentifier string          `json:"alertGroupIdentifier"`
	DisplayName          string          `json:"displayName"`
	Raw                  json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed fields and keeps the full record.
func (a *CaseAlert) UnmarshalJSON(b []byte) error {
	type alias CaseAlert
	var v alias
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*a = CaseAlert(v)
	a.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// ListCaseAlerts lists a case's alerts on the modern collection
// (cases/{case}/caseAlerts) — the source of the numeric caseAlert id.
func (c *Client) ListCaseAlerts(ctx context.Context, caseID string) ([]CaseAlert, error) {
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("soar: caseID is required")
	}
	var page struct {
		CaseAlerts []CaseAlert `json:"caseAlerts"`
		Items      []CaseAlert `json:"items"`
	}
	if err := c.t.V1Alpha(ctx, "GET", "cases/"+url.PathEscape(caseID)+"/caseAlerts", nil, &page); err != nil {
		return nil, err
	}
	if len(page.CaseAlerts) > 0 {
		return page.CaseAlerts, nil
	}
	return page.Items, nil
}

// CreateCaseAlertRecommendation starts AI recommendation generation for one
// alert in a case (caseAlerts:createRecommendationLongRunning) and returns the
// recommendation id to poll with FetchCaseAlertRecommendation. alertID is the
// caseAlert's NUMERIC id within the case (see ListCaseAlerts).
func (c *Client) CreateCaseAlertRecommendation(ctx context.Context, caseID, alertID string) (string, error) {
	if strings.TrimSpace(caseID) == "" || strings.TrimSpace(alertID) == "" {
		return "", fmt.Errorf("soar: caseID and alertID are required")
	}
	var out struct {
		RecommendationID string `json:"recommendationId"`
	}
	path := "cases/" + url.PathEscape(caseID) + "/caseAlerts/" + url.PathEscape(alertID) + ":createRecommendationLongRunning"
	if err := c.t.V1Alpha(ctx, "POST", path, map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.RecommendationID, nil
}

// AlertRecommendation is one polled caseAlerts:fetchRecommendation result.
// State runs RUNNING → SUCCEEDED | FAILED.
type AlertRecommendation struct {
	Recommendation string `json:"recommendation"`
	State          string `json:"state"`

	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed fields and keeps the full payload.
func (r *AlertRecommendation) UnmarshalJSON(b []byte) error {
	type alias AlertRecommendation
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = AlertRecommendation(a)
	r.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// Settled reports whether recommendation generation has finished.
func (r *AlertRecommendation) Settled() bool {
	return r != nil && (r.State == "SUCCEEDED" || r.State == "FAILED")
}

// FetchCaseAlertRecommendation polls one AI alert recommendation by id.
//
// The documented URL template nests a literal `caseAlerts:` segment under the
// caseAlert resource; some instances serve the verb directly on the resource
// instead, so a NotFound on the documented form falls back to the direct form.
func (c *Client) FetchCaseAlertRecommendation(ctx context.Context, caseID, alertID, recommendationID string) (*AlertRecommendation, error) {
	if strings.TrimSpace(recommendationID) == "" {
		return nil, fmt.Errorf("soar: recommendationID is required")
	}
	q := url.Values{"recommendationId": {recommendationID}}
	base := "cases/" + url.PathEscape(caseID) + "/caseAlerts/" + url.PathEscape(alertID)
	var out AlertRecommendation
	err := c.t.V1Alpha(ctx, "GET", base+"/caseAlerts:fetchRecommendation", nil, &out, transport.Query(q))
	if err != nil && IsNotFound(err) {
		err = c.t.V1Alpha(ctx, "GET", base+":fetchRecommendation", nil, &out, transport.Query(q))
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
