package chronicle

import (
	"bytes"
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
	CreateTime      string         `json:"createdTime,omitempty"`
	DetectionTime   string         `json:"detectionTime,omitempty"`
	CaseName        string         `json:"caseName,omitempty"`
	FeedbackSummary *AlertFeedback `json:"feedbackSummary,omitempty"`
	AlertCreateTime string         `json:"alertCreateTime,omitempty"`

	// Raw is the complete, untouched alert object as returned by the server
	// (detections, raw events, UDM, etc.). The typed fields above are a
	// convenience view; anything not modeled lives here.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON populates the typed fields and retains the full payload in Raw.
//
// The legacy alert endpoints have shipped two key spellings for the timestamps
// (live: createdTime/detectionTime; the older wrapper shape: createTime/
// detectionTimestamp). The struct tags target the live keys; this fills the typed
// fields from the older keys too, so the SDK decodes either shape.
func (a *Alert) UnmarshalJSON(b []byte) error {
	type alias Alert // avoid recursing into this method
	var v alias
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*a = Alert(v)
	a.Raw = append(a.Raw[:0], b...)
	if a.CreateTime == "" || a.DetectionTime == "" {
		var alt struct {
			CreateTime    string `json:"createTime"`
			DetectionTime string `json:"detectionTimestamp"`
		}
		_ = json.Unmarshal(b, &alt)
		if a.CreateTime == "" {
			a.CreateTime = alt.CreateTime
		}
		if a.DetectionTime == "" {
			a.DetectionTime = alt.DetectionTime
		}
	}
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
	SeverityDisplay string `json:"severityDisplay,omitempty"` // a label (e.g. "HIGH"), not a 0-100 score
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
	// legacyGetAlert may wrap the alert under an "alert" key (live) or return it
	// bare (older/wrapper shape); accept either so the inner object's typed fields
	// and Raw populate correctly.
	var raw json.RawMessage
	if err := c.get(ctx, c.alertPath("legacyGetAlert"), &raw, withQuery(q)); err != nil {
		return nil, err
	}
	var wrap struct {
		Alert json.RawMessage `json:"alert"`
	}
	if json.Unmarshal(raw, &wrap) == nil && len(wrap.Alert) > 0 {
		raw = wrap.Alert
	}
	var a Alert
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("chronicle: decode alert: %w", err)
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
		// legacyFetchAlertsView streams progressive snapshot fragments, returned as a
		// JSON array in a single response (the final fragment carries the complete
		// set). Older/wrapper shapes returned a single object — accept either. Fold
		// the fragments, keeping the latest non-empty values.
		var raw json.RawMessage
		if err := c.get(ctx, c.alertPath("legacyFetchAlertsView"), &raw, withQuery(q)); err != nil {
			return nil, err
		}
		var frags []alertsViewResponse
		if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
			if err := json.Unmarshal(raw, &frags); err != nil {
				return nil, fmt.Errorf("chronicle: decode alert view: %w", err)
			}
		} else {
			var one alertsViewResponse
			if err := json.Unmarshal(raw, &one); err != nil {
				return nil, fmt.Errorf("chronicle: decode alert view: %w", err)
			}
			frags = []alertsViewResponse{one}
		}
		for i := range frags {
			f := &frags[i]
			snap.Progress = f.Progress
			if f.Complete {
				snap.Complete = true
			}
			if f.FilteredAlertsCount > 0 {
				snap.FilteredAlertsCount = f.FilteredAlertsCount
			}
			if f.BaselineAlertsCount > 0 {
				snap.BaselineAlertsCount = f.BaselineAlertsCount
			}
			if len(f.FieldAggregations) > 0 {
				snap.FieldAggregations = f.FieldAggregations
			}
			if len(f.Alerts.Alerts) > 0 {
				snap.Alerts = f.Alerts.Alerts
			}
		}
		if snap.Complete {
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

// alertPath builds the legacy RPC path for an alert method:
// ".../instances/<id>/legacy:<method>" (project ID / string form).
func (c *Client) alertPath(method string) string {
	return c.resourcePath("legacy:"+method, false)
}
