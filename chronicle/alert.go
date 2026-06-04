package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Alerts use the project ID (string) form in their resource path (numeric=false):
// the wrapper builds every legacy alert URL from the string instance_id. The
// three operations are RPC-style "legacy:" methods hung off the instance:
//
//	{instance}/legacy:legacyGetAlert        (GET,  params: alertId[, includeDetections])
//	{instance}/legacy:legacyUpdateAlert     (POST, body: {alert_id, feedback{...}})
//	{instance}/legacy:legacyFetchAlertsView (GET,  streaming/polling alert snapshot)
//
// Unlike RPC methods that fuse to the instance with ':' (e.g. :verifyRuleText),
// these are "legacy:legacy..." — appended as a normal path segment after the
// instance, i.e. ".../instances/<id>/legacy:legacyGetAlert".

// Alert is a rule-detection alert. The API payload is large and only partially
// stable, so the curated fields below cover the common case and Raw retains the
// complete server object for callers that need anything else.
//
// DEVIATION: the wrapper returns a bare dict[str, Any]; we surface typed common
// fields while keeping the untouched payload in Raw (json.RawMessage) so nothing
// is lost and callers avoid map[string]any spelunking for the basics.
type Alert struct {
	ID              string         `json:"id,omitempty"`
	Type            string         `json:"type,omitempty"`
	CreateTime      string         `json:"createTime,omitempty"`
	DetectionTime   string         `json:"detectionTimestamp,omitempty"`
	CaseName        string         `json:"caseName,omitempty"`
	FeedbackSummary *AlertFeedback `json:"feedbackSummary,omitempty"`
	AlertCreateTime string         `json:"alertCreateTime,omitempty"`

	// Raw is the complete, untouched alert object as returned by the server
	// (detections, raw events, UDM, etc.). The typed fields above are a
	// convenience view; anything not modeled lives here.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON populates the typed fields and retains the full payload in Raw.
func (a *Alert) UnmarshalJSON(b []byte) error {
	type alias Alert // avoid recursing into this method
	var v alias
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*a = Alert(v)
	a.Raw = append(a.Raw[:0], b...)
	return nil
}

// AlertFeedback is the human/triage state attached to an alert. The fields
// mirror the legacy feedbackSummary object returned by the API.
type AlertFeedback struct {
	Status          string `json:"status,omitempty"`
	Priority        string `json:"priority,omitempty"`
	Verdict         string `json:"verdict,omitempty"`
	Reputation      string `json:"reputation,omitempty"`
	Reason          string `json:"reason,omitempty"`
	RiskScore       int    `json:"riskScore,omitempty"`
	Disregarded     bool   `json:"disregarded,omitempty"`
	Severity        int    `json:"severityDisplay,omitempty"`
	Comment         string `json:"comment,omitempty"`
	RootCause       string `json:"rootCause,omitempty"`
	ConfidenceScore int    `json:"confidenceScore,omitempty"`
}

// GetAlert fetches a single alert by ID via legacy:legacyGetAlert.
//
// Set includeDetections to fold the alert's detection details into the
// response. The returned Alert.Raw holds the complete server object.
func (c *Client) GetAlert(ctx context.Context, alertID string, includeDetections bool) (*Alert, error) {
	if strings.TrimSpace(alertID) == "" {
		return nil, &APIError{Method: "GET", URL: c.alertPath("legacyGetAlert"), Status: 0, Body: "alertID is required"}
	}
	q := url.Values{"alertId": {alertID}}
	if includeDetections {
		q.Set("includeDetections", "true")
	}
	var a Alert
	if err := c.get(ctx, c.alertPath("legacyGetAlert"), &a, withQuery(q)); err != nil {
		return nil, err
	}
	return &a, nil
}

// AlertsSnapshot is the result of a legacy:legacyFetchAlertsView call: the
// alert set matching snapshotQuery over the time range, plus progress metadata.
//
// Alerts and FieldAggregations are kept as RawMessage because their shapes are
// large, freeform, and version-dependent; Progress/Complete/counts are the
// stable fields callers poll on.
type AlertsSnapshot struct {
	Progress            float64         `json:"progress,omitempty"`
	Complete            bool            `json:"complete,omitempty"`
	Alerts              []Alert         `json:"-"`
	FilteredAlertsCount int             `json:"filteredAlertsCount,omitempty"`
	BaselineAlertsCount int             `json:"baselineAlertsCount,omitempty"`
	FieldAggregations   json.RawMessage `json:"-"`
}

// alertsViewResponse is the wire shape returned by legacyFetchAlertsView. The
// alert list is nested under "alerts.alerts" in the legacy payload.
type alertsViewResponse struct {
	Progress            float64         `json:"progress"`
	Complete            bool            `json:"complete"`
	FilteredAlertsCount int             `json:"filteredAlertsCount"`
	BaselineAlertsCount int             `json:"baselineAlertsCount"`
	FieldAggregations   json.RawMessage `json:"fieldAggregations"`
	Alerts              struct {
		Alerts []Alert `json:"alerts"`
	} `json:"alerts"`
}

// GetAlerts retrieves alerts matching snapshotQuery over [start, end) via
// legacy:legacyFetchAlertsView, polling until the server reports the snapshot
// is complete (or the poll budget is exhausted).
//
// snapshotQuery uses UDM-search-like syntax over alert fields
// (detection.rule_name, feedback_summary.status, etc.); pass "" to default to
// `feedback_summary.status != "CLOSED"`, matching the wrapper. baselineQuery is
// optional and, when set, is cached server-side for subsequent snapshot calls.
// maxAlerts caps returned alerts (<=0 leaves it to the server default of 1000).
// enableCache toggles server-side snapshot caching: nil omits the param (the
// server applies its own default), &true sends ALERTS_FEATURE_PREFERENCE_ENABLED
// and &false sends ...DISABLED — the wrapper's exact mapping.
//
// DEVIATION: the wrapper merges JSON fragments from a streaming response by
// string-patching commas; we instead poll the JSON endpoint (the foundation's
// do() decodes each complete response) and aggregate across pages until
// complete=true. Param names match the wrapper exactly.
func (c *Client) GetAlerts(ctx context.Context, start, end time.Time, maxAlerts int, snapshotQuery, baselineQuery string, enableCache *bool) (*AlertsSnapshot, error) {
	if snapshotQuery == "" {
		snapshotQuery = `feedback_summary.status != "CLOSED"`
	}
	q := url.Values{
		"timeRange.startTime": {start.UTC().Format(time.RFC3339)},
		"timeRange.endTime":   {end.UTC().Format(time.RFC3339)},
		"snapshotQuery":       {snapshotQuery},
	}
	if baselineQuery != "" {
		q.Set("baselineQuery", baselineQuery)
	}
	if enableCache != nil {
		v := "ALERTS_FEATURE_PREFERENCE_DISABLED"
		if *enableCache {
			v = "ALERTS_FEATURE_PREFERENCE_ENABLED"
		}
		q.Set("enableCache", v)
	}
	if maxAlerts > 0 {
		q.Set("alertListOptions.maxReturnedAlerts", strconv.Itoa(maxAlerts))
	}

	snap := &AlertsSnapshot{}
	const maxPolls = 30
	for attempt := range maxPolls {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		var resp alertsViewResponse
		if err := c.get(ctx, c.alertPath("legacyFetchAlertsView"), &resp, withQuery(q)); err != nil {
			return nil, err
		}
		snap.Progress = resp.Progress
		snap.Complete = resp.Complete
		snap.FilteredAlertsCount = resp.FilteredAlertsCount
		snap.BaselineAlertsCount = resp.BaselineAlertsCount
		if len(resp.FieldAggregations) > 0 {
			snap.FieldAggregations = resp.FieldAggregations
		}
		if len(resp.Alerts.Alerts) > 0 {
			snap.Alerts = resp.Alerts.Alerts
		}
		if resp.Complete {
			return snap, nil
		}
	}
	return nil, &APIError{
		Method: "GET",
		URL:    c.alertPath("legacyFetchAlertsView"),
		Status: 0,
		Body:   fmt.Sprintf("alert snapshot did not complete after %d polls", maxPolls),
	}
}

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

// validate checks enum membership and score ranges, returning a typed error on
// any invalid field and reporting whether at least one field is set.
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
// UpdateAlert per ID. We validate the update once up front (failing fast before
// any mutation) instead of re-validating on every iteration, and we stop on the
// first error — returning the alerts already updated plus the failing ID, so a
// partial failure is diagnosable rather than silently dropped.
func (c *Client) BulkUpdateAlerts(ctx context.Context, alertIDs []string, updates AlertUpdate) ([]*Alert, error) {
	if _, set, err := updates.build(); err != nil {
		return nil, err
	} else if !set {
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
		a, err := c.UpdateAlert(ctx, id, updates)
		if err != nil {
			return results, fmt.Errorf("chronicle: bulk update failed at alert %q: %w", id, err)
		}
		results = append(results, a)
	}
	return results, nil
}

// alertPath builds the legacy RPC path for an alert method:
// ".../instances/<id>/legacy:<method>" (project ID / string form).
func (c *Client) alertPath(method string) string {
	return c.resourcePath("legacy:"+method, false)
}
