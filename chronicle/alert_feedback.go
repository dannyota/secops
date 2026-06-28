package chronicle

import (
	"context"
	"fmt"
	"strings"
)

// AlertUpdate is a partial update to an alert's feedback. Pointer fields are
// "leave unchanged" when nil; *string comment/root_cause may point at "" to
// clear those fields explicitly (a non-nil empty string is sent).
type AlertUpdate struct {
	ConfidenceScore *int    // 0-100
	Reason          string  // REASON_UNSPECIFIED|REASON_NOT_MALICIOUS|REASON_MALICIOUS|REASON_MAINTENANCE
	Reputation      string  // REPUTATION_UNSPECIFIED|USEFUL|NOT_USEFUL
	Priority        string  // PRIORITY_UNSPECIFIED|PRIORITY_INFO|PRIORITY_LOW|PRIORITY_MEDIUM|PRIORITY_HIGH|PRIORITY_CRITICAL
	Status          string  // STATUS_UNSPECIFIED|NEW|REVIEWED|CLOSED|OPEN
	Verdict         string  // VERDICT_UNSPECIFIED|TRUE_POSITIVE|FALSE_POSITIVE
	RiskScore       *int    // 0-100
	Disregarded     *bool   // nil leaves unchanged
	Severity        *int    // 0-100
	Comment         *string // non-nil "" clears the analyst comment
	RootCause       *string // non-nil "" clears the root cause
}

// enum validation tables (mirror the wrapper's allowed values exactly).
var (
	alertPriorityValues = map[string]bool{
		"PRIORITY_UNSPECIFIED": true, "PRIORITY_INFO": true, "PRIORITY_LOW": true,
		"PRIORITY_MEDIUM": true, "PRIORITY_HIGH": true, "PRIORITY_CRITICAL": true,
	}
	alertReasonValues = map[string]bool{
		"REASON_UNSPECIFIED": true, "REASON_NOT_MALICIOUS": true,
		"REASON_MALICIOUS": true, "REASON_MAINTENANCE": true,
	}
	alertReputationValues = map[string]bool{
		"REPUTATION_UNSPECIFIED": true, "USEFUL": true, "NOT_USEFUL": true,
	}
	alertStatusValues = map[string]bool{
		"STATUS_UNSPECIFIED": true, "NEW": true, "REVIEWED": true, "CLOSED": true, "OPEN": true,
	}
	alertVerdictValues = map[string]bool{
		"VERDICT_UNSPECIFIED": true, "TRUE_POSITIVE": true, "FALSE_POSITIVE": true,
	}
)

// alertFeedbackBody is the JSON feedback object. Keys are snake_case to match
// the legacy:legacyUpdateAlert wire format the wrapper relies on.
type alertFeedbackBody struct {
	ConfidenceScore *int    `json:"confidence_score,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	Reputation      string  `json:"reputation,omitempty"`
	Priority        string  `json:"priority,omitempty"`
	Status          string  `json:"status,omitempty"`
	Verdict         string  `json:"verdict,omitempty"`
	RiskScore       *int    `json:"risk_score,omitempty"`
	Disregarded     *bool   `json:"disregarded,omitempty"`
	Severity        *int    `json:"severity,omitempty"`
	Comment         *string `json:"comment,omitempty"`
	RootCause       *string `json:"root_cause,omitempty"`
}

// build checks enum membership and score ranges, returning the wire body,
// whether at least one field is set, and any validation error.
func (u AlertUpdate) build() (alertFeedbackBody, bool, error) {
	var fb alertFeedbackBody
	set := false

	if u.Priority != "" {
		if !alertPriorityValues[u.Priority] {
			return fb, false, fmt.Errorf("chronicle: invalid alert priority %q", u.Priority)
		}
		fb.Priority = u.Priority
		set = true
	}
	if u.Reason != "" {
		if !alertReasonValues[u.Reason] {
			return fb, false, fmt.Errorf("chronicle: invalid alert reason %q", u.Reason)
		}
		fb.Reason = u.Reason
		set = true
	}
	if u.Reputation != "" {
		if !alertReputationValues[u.Reputation] {
			return fb, false, fmt.Errorf("chronicle: invalid alert reputation %q", u.Reputation)
		}
		fb.Reputation = u.Reputation
		set = true
	}
	if u.Status != "" {
		if !alertStatusValues[u.Status] {
			return fb, false, fmt.Errorf("chronicle: invalid alert status %q", u.Status)
		}
		fb.Status = u.Status
		set = true
	}
	if u.Verdict != "" {
		if !alertVerdictValues[u.Verdict] {
			return fb, false, fmt.Errorf("chronicle: invalid alert verdict %q", u.Verdict)
		}
		fb.Verdict = u.Verdict
		set = true
	}
	if u.ConfidenceScore != nil {
		if *u.ConfidenceScore < 0 || *u.ConfidenceScore > 100 {
			return fb, false, fmt.Errorf("chronicle: confidence_score must be 0-100, got %d", *u.ConfidenceScore)
		}
		fb.ConfidenceScore = u.ConfidenceScore
		set = true
	}
	if u.RiskScore != nil {
		if *u.RiskScore < 0 || *u.RiskScore > 100 {
			return fb, false, fmt.Errorf("chronicle: risk_score must be 0-100, got %d", *u.RiskScore)
		}
		fb.RiskScore = u.RiskScore
		set = true
	}
	if u.Severity != nil {
		if *u.Severity < 0 || *u.Severity > 100 {
			return fb, false, fmt.Errorf("chronicle: severity must be 0-100, got %d", *u.Severity)
		}
		fb.Severity = u.Severity
		set = true
	}
	if u.Disregarded != nil {
		fb.Disregarded = u.Disregarded
		set = true
	}
	if u.Comment != nil { // non-nil "" is a deliberate clear
		fb.Comment = u.Comment
		set = true
	}
	if u.RootCause != nil { // non-nil "" is a deliberate clear
		fb.RootCause = u.RootCause
		set = true
	}
	return fb, set, nil
}

// Validate checks the update client-side — enum membership, score ranges, and
// that at least one field is set — without making any API call. UpdateAlert
// runs the same checks; this lets a caller (e.g. a dry-run preview) fail fast
// before any guard or credential resolution.
func (u AlertUpdate) Validate() error {
	_, set, err := u.build()
	if err != nil {
		return err
	}
	if !set {
		return fmt.Errorf("chronicle: at least one alert property must be specified for update")
	}
	return nil
}

// alertUpdateBody is the legacy:legacyUpdateAlert request payload.
type alertUpdateBody struct {
	AlertID  string            `json:"alert_id"`
	Feedback alertFeedbackBody `json:"feedback"`
}

// UpdateAlert sets feedback on a single alert via legacy:legacyUpdateAlert.
//
// Only the fields set on updates are sent. Enum values and score ranges are
// validated client-side (matching the wrapper) before the call; at least one
// field must be set. Returns the updated alert payload.
func (c *Client) UpdateAlert(ctx context.Context, alertID string, updates AlertUpdate) (*Alert, error) {
	if strings.TrimSpace(alertID) == "" {
		return nil, &APIError{Method: "POST", URL: c.alertPath("legacyUpdateAlert"), Status: 0, Body: "alertID is required"}
	}
	fb, set, err := updates.build()
	if err != nil {
		return nil, err
	}
	if !set {
		return nil, &APIError{
			Method: "POST",
			URL:    c.alertPath("legacyUpdateAlert"),
			Status: 0,
			Body:   "at least one alert property must be specified for update",
		}
	}
	return c.updateAlertPrebuilt(ctx, alertID, fb)
}

// updateAlertPrebuilt posts an already-built (validated) feedback body for one
// alert — the shared core of UpdateAlert and BulkUpdateAlerts.
func (c *Client) updateAlertPrebuilt(ctx context.Context, alertID string, fb alertFeedbackBody) (*Alert, error) {
	body := alertUpdateBody{AlertID: alertID, Feedback: fb}
	var a Alert
	if err := c.post(ctx, c.alertPath("legacyUpdateAlert"), body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// BulkUpdateAlerts applies the same feedback to each alert in alertIDs.
//
// DEVIATION: like the wrapper, there is no batch endpoint, so this fans out to
// one update per ID. The update is built and validated once up front (failing
// fast before any mutation), and the loop stops on the first error — returning
// the alerts already updated plus the failing ID, so a partial failure is
// diagnosable rather than silently dropped.
func (c *Client) BulkUpdateAlerts(ctx context.Context, alertIDs []string, updates AlertUpdate) ([]*Alert, error) {
	fb, set, err := updates.build()
	if err != nil {
		return nil, err
	}
	if !set {
		return nil, &APIError{
			Method: "POST",
			URL:    c.alertPath("legacyUpdateAlert"),
			Status: 0,
			Body:   "at least one alert property must be specified for update",
		}
	}

	results := make([]*Alert, 0, len(alertIDs))
	for _, id := range alertIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		a, err := c.updateAlertPrebuilt(ctx, id, fb)
		if err != nil {
			return results, fmt.Errorf("chronicle: bulk update failed at alert %q: %w", id, err)
		}
		results = append(results, a)
	}
	return results, nil
}
