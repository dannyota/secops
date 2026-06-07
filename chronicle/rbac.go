// rbac.go — SIEM RBAC & data governance (SIEM plane).
//
// Data-access labels and scopes are the access-control primitives: a label tags
// data (by UDM query / log type), and a scope grants a role a set of allowed/
// denied labels. Both are clean per-object config-as-code targets.
// See docs/SURFACES.md.
//
// These resources are documented in the Chronicle v1alpha REST reference but are
// NOT in the official wrapper. The list endpoints are validated live; the per-
// object typed fields below are the standard name/displayName/description framing
// (full object detail is preserved in Raw and should be reconfirmed against a live
// object once the tenant has any).

package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// rbacAPIVersion (data-access + risk-config) is pinned in versions.go.

// DataAccessLabel tags data for access control (by UDM query / ingestion label).
type DataAccessLabel struct {
	Name        string          `json:"name"` // .../dataAccessLabels/{id}
	ID          string          `json:"-"`    // last name segment
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes typed fields, derives ID from the name, keeps Raw.
func (l *DataAccessLabel) UnmarshalJSON(data []byte) error {
	type alias DataAccessLabel
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*l = DataAccessLabel(a)
	l.ID = lastSegment(l.Name)
	l.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DataAccessScope grants a role a set of allowed/denied data-access labels.
type DataAccessScope struct {
	Name        string          `json:"name"` // .../dataAccessScopes/{id}
	ID          string          `json:"-"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes typed fields, derives ID from the name, keeps Raw.
func (s *DataAccessScope) UnmarshalJSON(data []byte) error {
	type alias DataAccessScope
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = DataAccessScope(a)
	s.ID = lastSegment(s.Name)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// --- data-access labels -----------------------------------------------------

// ListDataAccessLabels returns every data-access label. Read-only.
func (c *Client) ListDataAccessLabels(ctx context.Context) ([]DataAccessLabel, error) {
	var all []DataAccessLabel
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Labels        []DataAccessLabel `json:"dataAccessLabels"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("dataAccessLabels", false), &resp, withQuery(q), withVersion(rbacAPIVersion)); err != nil {
			return "", err
		}
		all = append(all, resp.Labels...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetDataAccessLabel fetches one label by short id or full resource name.
func (c *Client) GetDataAccessLabel(ctx context.Context, id string) (*DataAccessLabel, error) {
	var out DataAccessLabel
	if err := c.get(ctx, c.resourcePath("dataAccessLabels/"+url.PathEscape(lastSegment(id)), false), &out, withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDataAccessLabel creates a label with the given id and body (displayName /
// description / definition). The id becomes the trailing name segment. LIVE MUTATION.
func (c *Client) CreateDataAccessLabel(ctx context.Context, id string, body any) (*DataAccessLabel, error) {
	var out DataAccessLabel
	q := url.Values{"dataAccessLabelId": {id}}
	if err := c.post(ctx, c.resourcePath("dataAccessLabels", false), body, &out, withQuery(q), withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDataAccessLabel patches a label; mask lists the fields in body to update
// (updateMask). LIVE MUTATION.
func (c *Client) UpdateDataAccessLabel(ctx context.Context, id string, body any, mask ...string) (*DataAccessLabel, error) {
	var out DataAccessLabel
	var opts []requestOption
	if len(mask) > 0 {
		opts = append(opts, withQuery(url.Values{"updateMask": {strings.Join(mask, ",")}}))
	}
	if err := c.patch(ctx, c.resourcePath("dataAccessLabels/"+url.PathEscape(lastSegment(id)), false), body, &out, append(opts, withVersion(rbacAPIVersion))...); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDataAccessLabel deletes a label by id. LIVE MUTATION.
func (c *Client) DeleteDataAccessLabel(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("dataAccessLabels/"+url.PathEscape(lastSegment(id)), false), nil, nil, withVersion(rbacAPIVersion))
}

// --- data-access scopes -----------------------------------------------------

// ListDataAccessScopes returns every data-access scope. Read-only.
func (c *Client) ListDataAccessScopes(ctx context.Context) ([]DataAccessScope, error) {
	var all []DataAccessScope
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Scopes        []DataAccessScope `json:"dataAccessScopes"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("dataAccessScopes", false), &resp, withQuery(q), withVersion(rbacAPIVersion)); err != nil {
			return "", err
		}
		all = append(all, resp.Scopes...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetDataAccessScope fetches one scope by short id or full resource name.
func (c *Client) GetDataAccessScope(ctx context.Context, id string) (*DataAccessScope, error) {
	var out DataAccessScope
	if err := c.get(ctx, c.resourcePath("dataAccessScopes/"+url.PathEscape(lastSegment(id)), false), &out, withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDataAccessScope creates a scope with the given id and body (displayName /
// description / allowed-and-denied labels). LIVE MUTATION.
func (c *Client) CreateDataAccessScope(ctx context.Context, id string, body any) (*DataAccessScope, error) {
	var out DataAccessScope
	q := url.Values{"dataAccessScopeId": {id}}
	if err := c.post(ctx, c.resourcePath("dataAccessScopes", false), body, &out, withQuery(q), withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDataAccessScope patches a scope; mask lists the body fields to update. LIVE MUTATION.
func (c *Client) UpdateDataAccessScope(ctx context.Context, id string, body any, mask ...string) (*DataAccessScope, error) {
	var out DataAccessScope
	var opts []requestOption
	if len(mask) > 0 {
		opts = append(opts, withQuery(url.Values{"updateMask": {strings.Join(mask, ",")}}))
	}
	if err := c.patch(ctx, c.resourcePath("dataAccessScopes/"+url.PathEscape(lastSegment(id)), false), body, &out, append(opts, withVersion(rbacAPIVersion))...); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDataAccessScope deletes a scope by id. LIVE MUTATION.
func (c *Client) DeleteDataAccessScope(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("dataAccessScopes/"+url.PathEscape(lastSegment(id)), false), nil, nil, withVersion(rbacAPIVersion))
}

// --- risk config ------------------------------------------------------------

// RiskConfig is the instance-wide entity-risk-scoring configuration (a singleton
// sub-resource at {instance}/riskConfig). getRiskConfig returns system defaults
// when no custom config exists. Fields are pointers so a partial PATCH sends only
// what is set.
type RiskConfig struct {
	Name                          string   `json:"name,omitempty"`
	DefaultDetectionRiskScore     *float64 `json:"defaultDetectionRiskScore,omitempty"`
	DefaultAlertRiskScore         *float64 `json:"defaultAlertRiskScore,omitempty"`
	DefaultWeightingFactor        *float64 `json:"defaultWeightingFactor,omitempty"`
	DefaultClosedAlertCoefficient *float64 `json:"defaultClosedAlertCoefficient,omitempty"`
}

// GetRiskConfig returns the entity-risk-scoring config (GET {instance}/riskConfig).
// Read-only.
func (c *Client) GetRiskConfig(ctx context.Context) (*RiskConfig, error) {
	var out RiskConfig
	if err := c.get(ctx, c.resourcePath("riskConfig", false), &out, withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateRiskConfig patches the risk config (PATCH {instance}/riskConfig). The
// update is the full RiskConfig (the spec documents no updateMask query param).
// LIVE MUTATION.
func (c *Client) UpdateRiskConfig(ctx context.Context, cfg RiskConfig) (*RiskConfig, error) {
	var out RiskConfig
	if err := c.patch(ctx, c.resourcePath("riskConfig", false), cfg, &out, withVersion(rbacAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}
