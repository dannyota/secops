package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Rule exclusions (findings refinements) suppress detections/findings that
// match a UDM-style query. They live under the instance collection
// "findingsRefinements" and use the project ID (string) form in their resource
// path (numeric=false), matching the wrapper, whose SDK builds every instance
// URL from the string project_id. See resource.go for why the form is explicit
// per endpoint.

// RuleExclusionType is the type of a findings refinement.
// Valid values: DETECTION_EXCLUSION, FINDINGS_REFINEMENT_TYPE_UNSPECIFIED.
type RuleExclusionType string

const (
	// DetectionExclusion excludes matching detections from rules.
	DetectionExclusion RuleExclusionType = "DETECTION_EXCLUSION"
	// FindingsRefinementTypeUnspecified is the default/unset type.
	FindingsRefinementTypeUnspecified RuleExclusionType = "FINDINGS_REFINEMENT_TYPE_UNSPECIFIED"
)

// RuleExclusion is a findings refinement (rule exclusion).
//
// Name is the resource name projects/.../findingsRefinements/<id>; the trailing
// <id> segment is what Get/Patch/deployment/activity calls take (see ID).
// Etag is round-tripped on deployment updates for optimistic concurrency.
type RuleExclusion struct {
	Name        string            `json:"name,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Type        RuleExclusionType `json:"type,omitempty"`
	Query       string            `json:"query,omitempty"`
	Etag        string            `json:"etag,omitempty"`
	CreateTime  string            `json:"createTime,omitempty"`
	UpdateTime  string            `json:"updateTime,omitempty"`
}

// ID returns the trailing <id> segment of the exclusion's resource Name, the
// identifier the Get/Patch/deployment/activity paths expect.
func (e *RuleExclusion) ID() string {
	if e == nil || e.Name == "" {
		return ""
	}
	if i := strings.LastIndex(e.Name, "/findingsRefinements/"); i >= 0 {
		return e.Name[i+len("/findingsRefinements/"):]
	}
	return e.Name[strings.LastIndex(e.Name, "/")+1:]
}

// ListRuleExclusions returns every findings refinement (rule exclusion) in the
// instance, paginating over nextPageToken.
func (c *Client) ListRuleExclusions(ctx context.Context) ([]RuleExclusion, error) {
	var all []RuleExclusion
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			FindingsRefinements []RuleExclusion `json:"findingsRefinements"`
			NextPageToken       string          `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("findingsRefinements", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.FindingsRefinements...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetRuleExclusion fetches a single rule exclusion by its <id> segment.
func (c *Client) GetRuleExclusion(ctx context.Context, exclusionID string) (*RuleExclusion, error) {
	var ex RuleExclusion
	if err := c.get(ctx, c.resourcePath("findingsRefinements/"+exclusionID, false), &ex); err != nil {
		return nil, err
	}
	return &ex, nil
}

// CreateRuleExclusion creates a new findings refinement with the given display
// name, type, and query.
//
// DEVIATION: the wrapper sends snake_case "display_name"; the API also accepts
// the canonical camelCase "displayName", which we use here for consistency with
// the response shape and the rest of this SDK.
func (c *Client) CreateRuleExclusion(ctx context.Context, displayName string, refinementType RuleExclusionType, query string) (*RuleExclusion, error) {
	body := struct {
		DisplayName string            `json:"displayName"`
		Type        RuleExclusionType `json:"type"`
		Query       string            `json:"query"`
	}{DisplayName: displayName, Type: refinementType, Query: query}

	var ex RuleExclusion
	if err := c.post(ctx, c.resourcePath("findingsRefinements", false), body, &ex); err != nil {
		return nil, err
	}
	return &ex, nil
}

// RuleExclusionUpdate is a partial update to a rule exclusion. Only the set
// fields are sent and the updateMask is derived from them, so body and mask
// never drift.
type RuleExclusionUpdate struct {
	DisplayName string            // "" leaves the display name unchanged
	Type        RuleExclusionType // "" leaves the type unchanged
	Query       string            // "" leaves the query unchanged
}

// PatchRuleExclusion updates a rule exclusion, sending only the fields set on
// upd and an updateMask covering exactly those fields.
//
// DEVIATION: the wrapper takes a caller-supplied update_mask string; we build
// the mask from the populated fields so it cannot disagree with the body.
func (c *Client) PatchRuleExclusion(ctx context.Context, exclusionID string, upd RuleExclusionUpdate) (*RuleExclusion, error) {
	body := struct {
		DisplayName string            `json:"displayName,omitempty"`
		Type        RuleExclusionType `json:"type,omitempty"`
		Query       string            `json:"query,omitempty"`
	}{}
	var mask []string
	if upd.DisplayName != "" {
		body.DisplayName = upd.DisplayName
		mask = append(mask, "display_name")
	}
	if upd.Type != "" {
		body.Type = upd.Type
		mask = append(mask, "type")
	}
	if upd.Query != "" {
		body.Query = upd.Query
		mask = append(mask, "query")
	}
	path := c.resourcePath("findingsRefinements/"+exclusionID, false)
	if len(mask) == 0 {
		return nil, &APIError{Method: "PATCH", URL: path, Status: 0, Body: "no rule exclusion fields provided to update"}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var ex RuleExclusion
	if err := c.patch(ctx, path, body, &ex, withQuery(q)); err != nil {
		return nil, err
	}
	return &ex, nil
}

// RuleExclusionDeployment is the deployment (enable/archive) state of a rule
// exclusion.
//
// Name is the deployment resource projects/.../findingsRefinements/<id>/deployment.
// DetectionExclusionApplication is a freeform nested object (which curated rule
// sets the exclusion applies to) left as raw JSON so callers can pass it through
// unchanged. Etag is round-tripped on update for optimistic concurrency.
type RuleExclusionDeployment struct {
	Name                          string          `json:"name,omitempty"`
	Enabled                       bool            `json:"enabled,omitempty"`
	Archived                      bool            `json:"archived,omitempty"`
	ArchiveTime                   string          `json:"archiveTime,omitempty"`
	DetectionExclusionApplication json.RawMessage `json:"detectionExclusionApplication,omitempty"`
	Etag                          string          `json:"etag,omitempty"`
}

// GetRuleExclusionDeployment fetches the deployment state for a rule exclusion.
func (c *Client) GetRuleExclusionDeployment(ctx context.Context, exclusionID string) (*RuleExclusionDeployment, error) {
	var dep RuleExclusionDeployment
	if err := c.get(ctx, c.resourcePath("findingsRefinements/"+exclusionID+"/deployment", false), &dep); err != nil {
		return nil, err
	}
	return &dep, nil
}

// RuleExclusionDeploymentUpdate is a partial update to a rule exclusion's
// deployment. enabled and archived cannot both be true (mirrors the wrapper's
// validation). DetectionExclusionApplication, if set, is sent verbatim as raw
// JSON.
type RuleExclusionDeploymentUpdate struct {
	Enabled                       *bool           // nil leaves enabled unchanged
	Archived                      *bool           // nil leaves archived unchanged
	DetectionExclusionApplication json.RawMessage // nil/empty leaves it unchanged
	Etag                          string          // optional optimistic-concurrency etag
}

// UpdateRuleExclusionDeployment patches a rule exclusion's deployment, sending
// only the fields set on upd and an updateMask covering exactly those fields.
//
// DEVIATION: enabled+archived both true is rejected client-side (the wrapper
// raises the same), and the updateMask is built from the populated fields rather
// than caller-supplied, so body and mask cannot drift. The stored etag is
// round-tripped when provided for optimistic concurrency.
func (c *Client) UpdateRuleExclusionDeployment(ctx context.Context, exclusionID string, upd RuleExclusionDeploymentUpdate) (*RuleExclusionDeployment, error) {
	path := c.resourcePath("findingsRefinements/"+exclusionID+"/deployment", false)

	if upd.Enabled != nil && *upd.Enabled && upd.Archived != nil && *upd.Archived {
		return nil, &APIError{Method: "PATCH", URL: path, Status: 0, Body: "enabled and archived cannot both be true"}
	}

	body := struct {
		Enabled                       *bool           `json:"enabled,omitempty"`
		Archived                      *bool           `json:"archived,omitempty"`
		DetectionExclusionApplication json.RawMessage `json:"detectionExclusionApplication,omitempty"`
		Etag                          string          `json:"etag,omitempty"`
	}{Etag: upd.Etag}
	var mask []string
	if upd.Enabled != nil {
		body.Enabled = upd.Enabled
		mask = append(mask, "enabled")
	}
	if upd.Archived != nil {
		body.Archived = upd.Archived
		mask = append(mask, "archived")
	}
	if len(upd.DetectionExclusionApplication) > 0 {
		body.DetectionExclusionApplication = upd.DetectionExclusionApplication
		mask = append(mask, "detection_exclusion_application")
	}
	if len(mask) == 0 {
		return nil, &APIError{Method: "PATCH", URL: path, Status: 0, Body: "no deployment fields provided to update"}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var dep RuleExclusionDeployment
	if err := c.patch(ctx, path, body, &dep, withQuery(q)); err != nil {
		return nil, err
	}
	return &dep, nil
}

// RuleExclusionActivity is the result of computeFindingsRefinementActivity — the
// freeform activity statistics for a rule exclusion over an optional interval.
// Left as raw JSON since the response shape is reporting-only and not branched
// on by callers.
type RuleExclusionActivity = json.RawMessage

// ComputeRuleExclusionActivity computes activity statistics for a rule
// exclusion. A zero start/end omits that bound; passing both zero omits the
// interval entirely (whole-history stats).
//
// DEVIATION: the wrapper formats the interval with microsecond precision via
// strftime; we use RFC3339Nano (still RFC3339-compatible, UTC) for the
// interval.start_time/end_time fields the API expects.
func (c *Client) ComputeRuleExclusionActivity(ctx context.Context, exclusionID string, start, end time.Time) (RuleExclusionActivity, error) {
	type interval struct {
		StartTime string `json:"start_time,omitempty"`
		EndTime   string `json:"end_time,omitempty"`
	}
	body := struct {
		Interval *interval `json:"interval,omitempty"`
	}{}
	if !start.IsZero() || !end.IsZero() {
		iv := &interval{}
		if !start.IsZero() {
			iv.StartTime = start.UTC().Format(time.RFC3339Nano)
		}
		if !end.IsZero() {
			iv.EndTime = end.UTC().Format(time.RFC3339Nano)
		}
		body.Interval = iv
	}

	path := c.resourcePath("findingsRefinements/"+exclusionID, false) + ":computeFindingsRefinementActivity"
	var out json.RawMessage
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
