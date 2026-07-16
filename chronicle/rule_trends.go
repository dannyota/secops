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

// Rule-tuning reads: curated-rule detections, per-rule detection trends, rule
// count/quota stats, and the detection→events pivot. All are documented
// "legacy:" RPC GETs on the chronicle v1alpha instance path (string project id),
// following the rule_results.go pattern: stable envelopes typed, deep payloads
// kept as json.RawMessage. Plus rules:modifyRules — the batch rule-config
// update with per-item failure reporting.

// BucketSize values for the trends/buckets RPCs (the API documents day as the
// generally-supported size).
const (
	BucketSizeDay  = "BUCKET_SIZE_DAY"
	BucketSizeHour = "BUCKET_SIZE_HOUR"
)

// RuleSource values for SearchRuleDetectionCountBuckets, per the documented
// RuleSource enum: USER_RULE for customer rules, UPPERCASE_RULE for curated
// rules ("Uppercase" is the curated-content product name — not a typo).
const (
	RuleSourceUser    = "USER_RULE"
	RuleSourceCurated = "UPPERCASE_RULE"
)

// SearchCuratedDetections lists detections produced by a CURATED rule
// (`ur_…`) over [start, end) — the read `rules detections` cannot serve, since
// legacySearchDetections covers user rules only. Detections aggregate across
// all versions of the curated rule. alertState takes the AlertState* constants
// ("" omits the filter); pageSize <= 0 lets the server choose; results
// auto-paginate.
func (c *Client) SearchCuratedDetections(ctx context.Context, ruleID string, start, end time.Time, alertState string, pageSize int) ([]Detection, error) {
	if strings.TrimSpace(ruleID) == "" {
		return nil, fmt.Errorf("chronicle: curated rule id is required")
	}
	switch alertState {
	case "", AlertStateUnspecified, AlertStateNotAlerting, AlertStateAlerting:
	default:
		return nil, fmt.Errorf("chronicle: invalid alertState %q", alertState)
	}
	path := c.resourcePath("legacy:legacySearchCuratedDetections", false)
	var all []Detection
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{
			"ruleId":    {ruleID},
			"listBasis": {ListBasisDetectionTime},
		}
		if alertState != "" {
			q.Set("alertState", alertState)
		}
		if !start.IsZero() {
			q.Set("startTime", start.UTC().Format(time.RFC3339))
		}
		if !end.IsZero() {
			q.Set("endTime", end.UTC().Format(time.RFC3339))
		}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page struct {
			CuratedDetections []Detection `json:"curatedDetections"`
			NextPageToken     string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, path, &page, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, page.CuratedDetections...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// RuleTrend is one rule's detection-trend summary from the trends RPCs. The
// per-bucket counts vary in shape, so the buckets stay raw; the headline fields
// are decoded tolerantly (the LegacyRuleTrends message is loosely specified).
type RuleTrend struct {
	RuleID            string `json:"ruleId,omitempty"`
	LastDetectionTime string `json:"lastDetectionTime,omitempty"`

	// Raw is the complete trend object (bucketed counts and any other fields).
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed headline fields and keeps the whole object.
func (r *RuleTrend) UnmarshalJSON(b []byte) error {
	type alias RuleTrend
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = RuleTrend(a)
	r.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// TotalDetections sums the per-bucket detection counts — "how noisy was this
// rule over the window". (Bucket shape: detectionCounts[] of
// {timeRange, detectionCount, scannedStatus}; a zero-count bucket omits
// detectionCount.)
func (r *RuleTrend) TotalDetections() int {
	var probe struct {
		DetectionCounts []struct {
			DetectionCount int `json:"detectionCount"`
		} `json:"detectionCounts"`
	}
	if json.Unmarshal(r.Raw, &probe) != nil {
		return 0
	}
	total := 0
	for _, b := range probe.DetectionCounts {
		total += b.DetectionCount
	}
	return total
}

// trendsQuery builds the shared trends query: optional ruleIds, the required
// bucket window (proto Interval maps to dotted query params), and bucket size.
// The window is ALIGNED to whole buckets (floor start / ceil end) — the API
// returns a generic INVALID_ARGUMENT 400 for boundaries that are not aligned
// to the bucket size.
func trendsQuery(ruleIDs []string, start, end time.Time, bucketSize string) url.Values {
	q := url.Values{}
	for _, id := range ruleIDs {
		if id = strings.TrimSpace(id); id != "" {
			q.Add("ruleId", id)
		}
	}
	if bucketSize == "" {
		bucketSize = BucketSizeDay
	}
	unit := 24 * time.Hour
	if bucketSize == BucketSizeHour {
		unit = time.Hour
	}
	start = start.UTC().Truncate(unit)
	if !end.UTC().Equal(end.UTC().Truncate(unit)) {
		end = end.UTC().Truncate(unit).Add(unit)
	}
	q.Set("bucketTimeRange.startTime", start.Format(time.RFC3339))
	q.Set("bucketTimeRange.endTime", end.UTC().Format(time.RFC3339))
	q.Set("bucketSize", bucketSize)
	return q
}

// decodeRuleTrends decodes a trends response envelope.
func decodeRuleTrends(raw json.RawMessage) ([]RuleTrend, error) {
	var resp struct {
		RuleTrends []RuleTrend `json:"ruleTrends"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("chronicle: decode rule trends: %w", err)
	}
	return resp.RuleTrends, nil
}

// GetRulesTrends returns detection counts (bucketed over [start, end)) and the
// last-detection timestamp per USER rule — the "which of my rules are noisy or
// silent" read. An empty ruleIDs defaults to every rule on the instance.
func (c *Client) GetRulesTrends(ctx context.Context, ruleIDs []string, start, end time.Time, bucketSize string) ([]RuleTrend, error) {
	var raw json.RawMessage
	path := c.resourcePath("legacy:legacyGetRulesTrends", false)
	if err := c.get(ctx, path, &raw, withQuery(trendsQuery(ruleIDs, start, end, bucketSize))); err != nil {
		return nil, err
	}
	return decodeRuleTrends(raw)
}

// GetCuratedRulesTrends is GetRulesTrends for CURATED rules (`ur_…` ids, which
// are required here — the API does not default to all curated rules).
func (c *Client) GetCuratedRulesTrends(ctx context.Context, ruleIDs []string, start, end time.Time, bucketSize string) ([]RuleTrend, error) {
	if len(ruleIDs) == 0 {
		return nil, fmt.Errorf("chronicle: at least one curated rule id is required")
	}
	var raw json.RawMessage
	path := c.resourcePath("legacy:legacyGetCuratedRulesTrends", false)
	if err := c.get(ctx, path, &raw, withQuery(trendsQuery(ruleIDs, start, end, bucketSize))); err != nil {
		return nil, err
	}
	return decodeRuleTrends(raw)
}

// RuleCounts is the instance's rule count / quota statistics.
type RuleCounts struct {
	TotalActiveCount   int             `json:"totalActiveCount"`
	TotalArchivedCount int             `json:"totalArchivedCount"`
	TotalLiveRuleCount int             `json:"totalLiveRuleCount"`
	MaxLiveRuleCount   int             `json:"maxLiveRuleCount"`
	QuotaLimit         int             `json:"chronicleRulesQuotaLimit"`
	QuotaUsage         int             `json:"chronicleRulesQuotaUsage"`
	PerRuleType        json.RawMessage `json:"totalLiveRuleCountsPerRuleType,omitempty"`
	MaxPerRuleType     json.RawMessage `json:"maxLiveRuleCountsPerRuleType,omitempty"`
}

// GetRuleCounts returns the instance's rule count and quota stats.
func (c *Client) GetRuleCounts(ctx context.Context) (*RuleCounts, error) {
	var out RuleCounts
	if err := c.get(ctx, c.resourcePath("legacy:legacyGetRuleCounts", false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchRuleDetectionEvents returns the events behind one USER-rule detection
// (the detection→evidence pivot): maps of event/entity/detection samples keyed
// by event variable, kept raw (UDM events are deep and schema-heavy).
// maxEvents <= 0 leaves the server default (100k cap).
func (c *Client) SearchRuleDetectionEvents(ctx context.Context, ruleID, detectionID string, maxEvents int) (json.RawMessage, error) {
	if strings.TrimSpace(ruleID) == "" || strings.TrimSpace(detectionID) == "" {
		return nil, fmt.Errorf("chronicle: ruleID and detectionID are required")
	}
	q := url.Values{"ruleId": {ruleID}, "detectionId": {detectionID}}
	if maxEvents > 0 {
		q.Set("maxEvents", strconv.Itoa(maxEvents))
	}
	var out json.RawMessage
	path := c.resourcePath("legacy:legacySearchRuleDetectionEvents", false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEventForDetection returns the event(s) and rationale behind one CURATED
// detection (the curated twin of SearchRuleDetectionEvents). pageSize <= 0
// leaves the server default (1000).
func (c *Client) GetEventForDetection(ctx context.Context, detectionID string, pageSize int) (json.RawMessage, error) {
	if strings.TrimSpace(detectionID) == "" {
		return nil, fmt.Errorf("chronicle: detectionID is required")
	}
	q := url.Values{"detectionId": {detectionID}}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	var out json.RawMessage
	path := c.resourcePath("legacy:legacyGetEventForDetection", false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchRuleDetectionCountBuckets returns per-day detection count buckets for
// one rule over [start, end). ruleSource must match the id form: RuleSourceUser
// for `ru_…` rules (the default), RuleSourceCurated for `ur_…` curated rules.
func (c *Client) SearchRuleDetectionCountBuckets(ctx context.Context, ruleID string, start, end time.Time, ruleSource string) (json.RawMessage, error) {
	if strings.TrimSpace(ruleID) == "" {
		return nil, fmt.Errorf("chronicle: ruleID is required")
	}
	// Day-aligned boundaries, like the trends RPCs (unaligned -> generic 400).
	start = start.UTC().Truncate(24 * time.Hour)
	if !end.UTC().Equal(end.UTC().Truncate(24 * time.Hour)) {
		end = end.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	q := url.Values{
		"ruleId":              {ruleID},
		"timeRange.startTime": {start.Format(time.RFC3339)},
		"timeRange.endTime":   {end.Format(time.RFC3339)},
		"bucketSize":          {BucketSizeDay},
	}
	if ruleSource != "" {
		q.Set("ruleSource", ruleSource)
	}
	var out json.RawMessage
	path := c.resourcePath("legacy:legacySearchRuleDetectionCountBuckets", false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// RuleUpdateRequest is one entry of a ModifyRules batch: a partial Rule body
// (carrying `name` plus the fields named by UpdateMask) and the mask.
type RuleUpdateRequest struct {
	Rule       json.RawMessage `json:"rule"`
	UpdateMask string          `json:"updateMask,omitempty"`
}

// ModifyRules applies a batch of rule updates in one call
// (POST {parent}/rules:modifyRules). The response carries the updated rules
// plus a per-index failure map, so a partial failure is attributable — unlike
// a per-rule PATCH loop, which stops mid-sweep. LIVE MUTATION: callers gate it
// behind the standard guard.
func (c *Client) ModifyRules(ctx context.Context, requests []RuleUpdateRequest) (json.RawMessage, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("chronicle: at least one rule update is required")
	}
	body := struct {
		Requests []RuleUpdateRequest `json:"requests"`
	}{Requests: requests}
	var out json.RawMessage
	if err := c.post(ctx, c.resourcePath("rules:modifyRules", false), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
