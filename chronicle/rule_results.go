package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Rule detections, execution errors, and rule-generated alerts.
//
// These all read from "legacy" detection-engine endpoints, which the wrapper
// builds from the string project_id instance path (numeric=false). The detection
// and alert payloads are deeply nested and only loosely specified by the API, so
// the top-level, stable fields are typed and the variable inner blobs are kept as
// json.RawMessage rather than forced into brittle structs.

// Detection alert-state filter / list-basis enum values (mirrors the wrapper's
// validation lists). Exported as constants so callers don't hand-type strings.
const (
	AlertStateUnspecified = "UNSPECIFIED"
	AlertStateNotAlerting = "NOT_ALERTING"
	AlertStateAlerting    = "ALERTING"

	ListBasisUnspecified   = "LIST_BASIS_UNSPECIFIED"
	ListBasisCreatedTime   = "CREATED_TIME"
	ListBasisDetectionTime = "DETECTION_TIME"
)

// detectionTimeLayout matches the wrapper's strftime("%Y-%m-%dT%H:%M:%S.%fZ")
// for legacySearchDetections start/end params (microseconds, literal Z).
const detectionTimeLayout = "2006-01-02T15:04:05.000000Z"

// DetectionMatch is one per-rule match summary inside a Detection (the nested
// "detection" array — yes, the API nests "detection" under "detections").
type DetectionMatch struct {
	RuleID      string `json:"ruleId,omitempty"`
	RuleVersion string `json:"ruleVersion,omitempty"`
	RuleName    string `json:"ruleName,omitempty"`
	Description string `json:"description,omitempty"`
	AlertState  string `json:"alertState,omitempty"` // UNSPECIFIED | NOT_ALERTING | ALERTING
	RuleType    string `json:"ruleType,omitempty"`   // e.g. SINGLE_EVENT | MULTI_EVENT
	// URLBackToProduct, ruleLabels, severity, etc. vary by rule; keep raw.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed fields and preserves the whole match object in
// Raw, so the rule-dependent extras (URLBackToProduct, ruleLabels, severity, …)
// are available rather than silently dropped. Mirrors the json:"-"+Raw idiom
// used by DataTableColumn, Alert, Case, etc.
func (m *DetectionMatch) UnmarshalJSON(b []byte) error {
	type alias DetectionMatch
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = DetectionMatch(a)
	m.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// DetectionTimeWindow is the [start, end) window a detection covered.
type DetectionTimeWindow struct {
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

// Detection is one result row from legacySearchDetections.
//
// The stable envelope fields are typed; the heavyweight event evidence
// (collectionElements / resultEvents, when requested) is preserved verbatim in
// CollectionElements so callers can decode it on demand without this SDK pinning
// a UDM-event schema.
type Detection struct {
	ID            string               `json:"id,omitempty"`
	Type          string               `json:"type,omitempty"` // e.g. RULE_DETECTION
	CreatedTime   string               `json:"createdTime,omitempty"`
	DetectionTime string               `json:"detectionTime,omitempty"`
	TimeWindow    *DetectionTimeWindow `json:"timeWindow,omitempty"`
	Detection     []DetectionMatch     `json:"detection,omitempty"`

	// Freeform, deeply-nested event evidence; absent unless populated by the API.
	CollectionElements json.RawMessage `json:"collectionElements,omitempty"`
}

// ListDetections returns detections produced by ruleID over [start, end].
//
// ruleID accepts the wrapper's forms: "{rule_id}" (latest), "{rule_id}@v_..."
// (a version), or "{rule_id}@-" (all versions). A zero start/end omits that
// bound. alertState, if non-empty, must be one of the AlertState* constants.
// pageSize <= 0 lets the server choose; results are auto-paginated.
//
// DEVIATION: list_basis is fixed to DETECTION_TIME (the most useful ordering for
// time-bounded queries) rather than exposed as a parameter; the wrapper defaults
// it to LIST_BASIS_UNSPECIFIED. Callers needing another basis can be added later.
func (c *Client) ListDetections(ctx context.Context, ruleID string, start, end time.Time, alertState string, pageSize int) ([]Detection, error) {
	switch alertState {
	case "", AlertStateUnspecified, AlertStateNotAlerting, AlertStateAlerting:
	default:
		return nil, fmt.Errorf("chronicle: invalid alertState %q (want %s, %s, or %s)",
			alertState, AlertStateUnspecified, AlertStateNotAlerting, AlertStateAlerting)
	}

	// legacy path: {instance}/legacy:legacySearchDetections (path separator, then
	// the RPC verb), per the wrapper's chronicle_request URL rules.
	path := c.resourcePath("legacy:legacySearchDetections", false)

	var all []Detection
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{
			"rule_id":   {ruleID},
			"listBasis": {ListBasisDetectionTime},
		}
		if alertState != "" {
			q.Set("alertState", alertState)
		}
		if !start.IsZero() {
			q.Set("startTime", start.UTC().Format(detectionTimeLayout))
		}
		if !end.IsZero() {
			q.Set("endTime", end.UTC().Format(detectionTimeLayout))
		}
		if pageSize > 0 {
			q.Set("pageSize", fmt.Sprintf("%d", pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}

		var resp struct {
			Detections    []Detection `json:"detections"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, path, &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Detections...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// RuleExecution is the rule-run context attached to a RuleError's metadata.
type RuleExecution struct {
	WindowStartTime string `json:"windowStartTime,omitempty"`
	WindowEndTime   string `json:"windowEndTime,omitempty"`
	RuleID          string `json:"ruleId,omitempty"`
	VersionID       string `json:"versionId,omitempty"`
}

// RuleErrorMetadata wraps the ruleExecution context of a RuleError.
type RuleErrorMetadata struct {
	RuleExecution *RuleExecution `json:"ruleExecution,omitempty"`
}

// RuleError is a single rule execution error from the ruleExecutionErrors
// collection.
//
// Field names span two API generations: v1alpha returns name/error, the older
// shape returns errorId/text. Both are decoded so callers see a populated
// message regardless of which the instance emits (use Message()).
type RuleError struct {
	Name      string             `json:"name,omitempty"`     // v1alpha resource name
	ErrorID   string             `json:"errorId,omitempty"`  // legacy id form
	Error     string             `json:"error,omitempty"`    // v1alpha message
	Text      string             `json:"text,omitempty"`     // legacy message
	Category  string             `json:"category,omitempty"` // e.g. RULES_EXECUTION_ERROR
	ErrorTime string             `json:"errorTime,omitempty"`
	Metadata  *RuleErrorMetadata `json:"metadata,omitempty"`
}

// Message returns the human-readable error text across either API shape.
func (e *RuleError) Message() string {
	if e == nil {
		return ""
	}
	if e.Error != "" {
		return e.Error
	}
	return e.Text
}

// ListErrors returns execution errors for ruleID. ruleID accepts the same forms
// as ListDetections ("{id}", "{id}@v_...", "{id}@-").
//
// DEVIATION: the start/end params are accepted for a consistent signature but the
// underlying ruleExecutionErrors endpoint filters only by rule (no time range in
// the wrapper); they are ignored here. The server-side filter is built exactly as
// the wrapper does: rule = "{instance}/rules/{ruleID}".
func (c *Client) ListErrors(ctx context.Context, ruleID string, start, end time.Time) ([]RuleError, error) {
	_ = start
	_ = end

	filter := fmt.Sprintf("rule = %q", c.instancePath(false)+"/rules/"+ruleID)
	q := url.Values{"filter": {filter}}

	// Paginated for safety, though the wrapper issues a single request.
	var all []RuleError
	err := paginate(50, func(token string) (string, error) {
		qq := cloneValues(q)
		if token != "" {
			qq.Set("pageToken", token)
		}
		var resp struct {
			RuleExecutionErrors []RuleError `json:"ruleExecutionErrors"`
			NextPageToken       string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("ruleExecutionErrors", false), &resp, withQuery(qq)); err != nil {
			return "", err
		}
		all = append(all, resp.RuleExecutionErrors...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// RuleAlertSearch is the result of SearchRuleAlerts. Each RuleAlerts element is
// an opaque "ruleAlert" object (alerts[] + ruleMetadata). TooManyAlerts is true
// when the server truncated the (non-paginated) result set — narrow the time
// range to see the rest.
type RuleAlertSearch struct {
	RuleAlerts    []json.RawMessage
	TooManyAlerts bool
}

// SearchRuleAlerts returns alerts generated by rules over [start, end).
//
// Each element is a "ruleAlert" object whose shape is deeply nested and
// rule-dependent; it is returned verbatim as json.RawMessage so callers decode
// only what they need. The endpoint is a single legacy GET with no pagination
// token, so the server's tooManyAlerts truncation flag is surfaced instead.
func (c *Client) SearchRuleAlerts(ctx context.Context, ruleID string, start, end time.Time) (*RuleAlertSearch, error) {
	_ = ruleID // DEVIATION: the legacy endpoint searches all rules; it has no per-rule filter.

	q := url.Values{
		"timeRange.start_time": {start.UTC().Format(time.RFC3339)},
		"timeRange.end_time":   {end.UTC().Format(time.RFC3339)},
	}

	var resp struct {
		RuleAlerts    []json.RawMessage `json:"ruleAlerts"`
		TooManyAlerts bool              `json:"tooManyAlerts"`
	}
	if err := c.get(ctx, c.resourcePath("legacy:legacySearchRulesAlerts", false), &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return &RuleAlertSearch{RuleAlerts: resp.RuleAlerts, TooManyAlerts: resp.TooManyAlerts}, nil
}

// cloneValues returns a shallow copy of v so per-page mutations don't leak.
func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[k] = cp
	}
	return out
}
