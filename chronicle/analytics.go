// analytics.go — Wave 17: flagship analytics & AI READ surfaces on the SIEM
// (chronicle/ADC) plane — the Gemini Triage & Investigation Agent (investigations
// + steps + comments), entity risk scores, BigQuery-export status, and MITRE
// coverage details. Read-only: the investigations :trigger / :transitionReviewState
// and bigQueryExport :provision / update writes are intentionally NOT wired (they
// are gated, side-effecting mutations). Records are returned raw (the shapes are
// large and still evolving). See docs/design/surfaces.md and design/architecture.md §6.

package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// listRawCollection GETs a chronicle collection at sub, accumulating the array
// under envelope key `key` and following nextPageToken (cap 50 pages). version=""
// rides DefaultAPIVersion; extra carries any additional query params (filter,
// orderBy, …) merged into each page request.
func (c *Client) listRawCollection(ctx context.Context, sub, key string, pageSize int, version string, numeric bool, extra url.Values) ([]json.RawMessage, error) {
	var all []json.RawMessage
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		for k, vs := range extra {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		opts := []requestOption{withQuery(q)}
		if version != "" {
			opts = append(opts, withVersion(version))
		}
		var env map[string]json.RawMessage
		if err := c.get(ctx, c.resourcePath(sub, numeric), &env, opts...); err != nil {
			return "", err
		}
		if items, ok := env[key]; ok && len(items) > 0 {
			var page []json.RawMessage
			if err := json.Unmarshal(items, &page); err != nil {
				return "", err
			}
			all = append(all, page...)
		}
		var next string
		if t, ok := env["nextPageToken"]; ok {
			_ = json.Unmarshal(t, &next)
		}
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// Investigations (the Gemini Triage & Investigation Agent / TIN) list/get/trigger
// live in investigations.go; these add the sub-resource step/comment reads.

// ListInvestigationSteps returns the Gemini-performed steps of an investigation. Read-only.
func (c *Client) ListInvestigationSteps(ctx context.Context, investigationID string, pageSize int) ([]json.RawMessage, error) {
	sub := "investigations/" + url.PathEscape(lastSegment(investigationID)) + "/investigationSteps"
	return c.listRawCollection(ctx, sub, "investigationSteps", pageSize, "", false, nil)
}

// ListInvestigationComments returns the comments on an investigation. Read-only.
func (c *Client) ListInvestigationComments(ctx context.Context, investigationID string, pageSize int) ([]json.RawMessage, error) {
	sub := "investigations/" + url.PathEscape(lastSegment(investigationID)) + "/investigationComments"
	return c.listRawCollection(ctx, sub, "investigationComments", pageSize, "", false, nil)
}

// EntityRiskScore is a per-entity behavioral risk score with detection context.
type EntityRiskScore struct {
	Entity             EntityIndicatorWrap `json:"entity"`
	RiskWindow         TimeWindow          `json:"riskWindow"`
	RiskScore          int                 `json:"riskScore"`
	RiskDelta          RiskDelta           `json:"riskDelta"`
	DetectionsCount    int                 `json:"detectionsCount"`
	FirstDetectionTime string              `json:"firstDetectionTime"`
	LastDetectionTime  string              `json:"lastDetectionTime"`
	EntityIndicator    map[string]string   `json:"entityIndicator"`
	RawRiskScore       int                 `json:"rawRiskScore"`
	RawRiskDelta       RiskDelta           `json:"rawRiskDelta"`
	EntityID           string              `json:"entityId"`
	Raw                json.RawMessage     `json:"-"`
}

// UnmarshalJSON preserves the raw bytes alongside the typed fields.
func (e *EntityRiskScore) UnmarshalJSON(b []byte) error {
	type alias EntityRiskScore
	if err := json.Unmarshal(b, (*alias)(e)); err != nil {
		return err
	}
	e.Raw = append(e.Raw[:0:0], b...)
	return nil
}

// EntityIndicatorWrap is the entity wrapper in a risk-score record.
type EntityIndicatorWrap struct {
	Metadata struct {
		EntityType string `json:"entityType"`
	} `json:"metadata"`
}

// TimeWindow is a half-open [start, end) time interval.
type TimeWindow struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// RiskDelta holds the percentage delta of a risk score.
type RiskDelta struct {
	RiskScoreDelta int `json:"riskScoreDelta"`
}

// QueryEntityRiskScores returns entity risk scores (normalized 0–1000) for the
// instance, optionally filtered/ordered. filter uses the documented expression
// syntax (e.g. `riskScore>=50`); orderBy e.g. "detectionsCount". Issues
// GET {instance}/entityRiskScores:query with an empty body. Read-only (v1alpha).
func (c *Client) QueryEntityRiskScores(ctx context.Context, filter, orderBy string, pageSize int) ([]EntityRiskScore, error) {
	extra := url.Values{}
	if filter != "" {
		extra.Set("filter", filter)
	}
	if orderBy != "" {
		extra.Set("orderBy", orderBy)
	}
	raw, err := c.listRawCollection(ctx, "entityRiskScores:query", "entityRiskScores", pageSize, "", false, extra)
	if err != nil {
		return nil, err
	}
	scores := make([]EntityRiskScore, len(raw))
	for i, r := range raw {
		if err := json.Unmarshal(r, &scores[i]); err != nil {
			return nil, err
		}
	}
	return scores, nil
}

// GetBigQueryExport returns the Advanced BigQuery Export configuration singleton
// (raw). Read-only; provision/update are gated mutations and not wired. May 404 /
// permission-deny on instances without the feature (Enterprise Plus, Pre-GA) —
// surface the typed *APIError cleanly. Pinned v1.
func (c *Client) GetBigQueryExport(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.get(ctx, c.resourcePath("bigQueryExport", false), &out, withVersion(bigQueryExportAPIVersion)); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCoverageDetails returns MITRE ATT&CK coverage per threat-collection × rule
// combination (raw) — the API-side view of detection/"emerging threats" coverage.
// Read-only. Pinned v1.
func (c *Client) ListCoverageDetails(ctx context.Context, pageSize int) ([]json.RawMessage, error) {
	// coverageDetails wants the project-number form, matching ti.go.
	return c.listRawCollection(ctx, "coverageDetails", "coverageDetails", pageSize, coverageAPIVersion, true, nil)
}
