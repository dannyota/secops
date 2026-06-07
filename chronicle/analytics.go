// analytics.go — Wave 17: flagship analytics & AI READ surfaces on the SIEM
// (chronicle/ADC) plane — the Gemini Triage & Investigation Agent (investigations
// + steps + comments), entity risk scores, BigQuery-export status, and MITRE
// coverage details. Read-only: the investigations :trigger / :transitionReviewState
// and bigQueryExport :provision / update writes are intentionally NOT wired (they
// are gated, side-effecting mutations). Records are returned raw (the shapes are
// large and still evolving). See docs/SURFACES.md and ARCHITECTURE.md §6.

package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// listRawCollection GETs a chronicle collection at sub, accumulating the array
// under envelope key `key` and following nextPageToken (cap 50 pages). version=""
// rides DefaultAPIVersion.
func (c *Client) listRawCollection(ctx context.Context, sub, key string, pageSize int, version string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
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
		if err := c.get(ctx, c.resourcePath(sub, false), &env, opts...); err != nil {
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
	return c.listRawCollection(ctx, sub, "investigationSteps", pageSize, "")
}

// ListInvestigationComments returns the comments on an investigation. Read-only.
func (c *Client) ListInvestigationComments(ctx context.Context, investigationID string, pageSize int) ([]json.RawMessage, error) {
	sub := "investigations/" + url.PathEscape(lastSegment(investigationID)) + "/investigationComments"
	return c.listRawCollection(ctx, sub, "investigationComments", pageSize, "")
}

// QueryEntityRiskScores returns entity risk scores (normalized 0–1000) for the
// instance, optionally filtered/ordered. filter uses the documented expression
// syntax (e.g. `riskScore>=50`); orderBy e.g. "detectionsCount". Issues
// GET {instance}/entityRiskScores:query with an empty body. Read-only (v1alpha).
func (c *Client) QueryEntityRiskScores(ctx context.Context, filter, orderBy string, pageSize int) ([]json.RawMessage, error) {
	var all []json.RawMessage
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if filter != "" {
			q.Set("filter", filter)
		}
		if orderBy != "" {
			q.Set("orderBy", orderBy)
		}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var env struct {
			EntityRiskScores []json.RawMessage `json:"entityRiskScores"`
			NextPageToken    string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("entityRiskScores:query", false), &env, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, env.EntityRiskScores...)
		return env.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
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
	return c.listRawCollection(ctx, "coverageDetails", "coverageDetails", pageSize, coverageAPIVersion)
}
