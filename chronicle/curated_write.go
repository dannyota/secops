package chronicle

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Curated rule-set deployment writes. The curated catalog is Google-managed: an
// operator cannot create or delete rule sets, only flip two booleans — enabled
// and alerting — per (category, rule set, precision) deployment. precision is one
// of "precise" or "broad". These use the project-ID (string) form (numeric=false),
// matching ListCuratedRuleSetDeployments.

// curatedPrecisions is the set of valid precision tiers for a deployment.
var curatedPrecisions = map[string]bool{"precise": true, "broad": true}

// validateCuratedPrecision rejects a precision the server would reject.
func validateCuratedPrecision(p string) error {
	if !curatedPrecisions[p] {
		return fmt.Errorf("chronicle: invalid curated precision %q: want \"precise\" or \"broad\"", p)
	}
	return nil
}

// ValidateCuratedPrecision reports whether p is a valid precision tier
// ("precise" or "broad"), so a caller can fail fast (e.g. before a guard banner)
// without constructing a client.
func ValidateCuratedPrecision(p string) error { return validateCuratedPrecision(p) }

// CuratedDeploymentUpdate is a partial toggle of a curated rule-set deployment.
// Only the non-nil fields are sent, and the updateMask is derived from exactly
// those, so an unset field is never overwritten.
type CuratedDeploymentUpdate struct {
	Enabled  *bool // nil leaves enabled unchanged
	Alerting *bool // nil leaves alerting unchanged
}

// curatedDeploymentPath builds the relative deployment resource sub-path.
func curatedDeploymentPath(categoryID, ruleSetID, precision string) string {
	return "curatedRuleSetCategories/" + categoryID +
		"/curatedRuleSets/" + ruleSetID +
		"/curatedRuleSetDeployments/" + precision
}

// UpdateCuratedRuleSetDeployment toggles enabled/alerting on one curated rule-set
// deployment. At least one of upd.Enabled/upd.Alerting must be set. The updated
// deployment is returned.
//
// DEVIATION: the official wrapper omits the updateMask on the single PATCH and
// lets the server infer it; we always send the mask built from the set fields
// (matching UpdateRuleDeployment / UpdateDataTable) so body and mask never drift.
func (c *Client) UpdateCuratedRuleSetDeployment(ctx context.Context, categoryID, ruleSetID, precision string, upd CuratedDeploymentUpdate) (*CuratedRuleSetDeployment, error) {
	if err := validateCuratedPrecision(precision); err != nil {
		return nil, err
	}
	body := struct {
		Enabled  *bool `json:"enabled,omitempty"`
		Alerting *bool `json:"alerting,omitempty"`
	}{}
	var mask []string
	if upd.Enabled != nil {
		body.Enabled = upd.Enabled
		mask = append(mask, "enabled")
	}
	if upd.Alerting != nil {
		body.Alerting = upd.Alerting
		mask = append(mask, "alerting")
	}
	if len(mask) == 0 {
		return nil, &APIError{
			Method: "PATCH",
			URL:    c.resourcePath(curatedDeploymentPath(categoryID, ruleSetID, precision), false),
			Body:   "no curated deployment fields provided to update",
		}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var dep CuratedRuleSetDeployment
	if err := c.patch(ctx, c.resourcePath(curatedDeploymentPath(categoryID, ruleSetID, precision), false), body, &dep, withQuery(q)); err != nil {
		return nil, err
	}
	return &dep, nil
}

// CuratedDeploymentChange is one entry in a batch curated-deployment update: the
// (category, rule set, precision) to target and the enabled/alerting state to set.
type CuratedDeploymentChange struct {
	CategoryID string
	RuleSetID  string
	Precision  string
	Enabled    bool
	Alerting   bool
}

// BatchUpdateCuratedRuleSetDeployments sets enabled/alerting on many curated
// rule-set deployments in one atomic call — the write primitive for reconciling a
// whole curated-deployment desired-state file (vs N single PATCHes). LIVE MUTATION.
//
// Endpoint: POST {instance}/curatedRuleSetCategories/-/curatedRuleSets/-/
// curatedRuleSetDeployments:batchUpdate, body {parent, requests:[{
// curatedRuleSetDeployment:{name,enabled,alerting}, updateMask:{paths}}]} — each
// name is the full deployment resource name. Project-ID form (numeric=false),
// matching the single-deployment PATCH.
func (c *Client) BatchUpdateCuratedRuleSetDeployments(ctx context.Context, changes []CuratedDeploymentChange) ([]CuratedRuleSetDeployment, error) {
	type fieldMask struct {
		Paths []string `json:"paths"`
	}
	type reqItem struct {
		Deployment CuratedRuleSetDeployment `json:"curatedRuleSetDeployment"`
		UpdateMask fieldMask                `json:"updateMask"`
	}
	items := make([]reqItem, 0, len(changes))
	for _, ch := range changes {
		if err := validateCuratedPrecision(ch.Precision); err != nil {
			return nil, err
		}
		items = append(items, reqItem{
			Deployment: CuratedRuleSetDeployment{
				Name:     c.resourcePath(curatedDeploymentPath(ch.CategoryID, ch.RuleSetID, ch.Precision), false),
				Enabled:  ch.Enabled,
				Alerting: ch.Alerting,
			},
			UpdateMask: fieldMask{Paths: []string{"enabled", "alerting"}},
		})
	}
	body := struct {
		Parent   string    `json:"parent"`
		Requests []reqItem `json:"requests"`
	}{
		Parent:   c.resourcePath("curatedRuleSetCategories/-/curatedRuleSets/-", false),
		Requests: items,
	}

	var resp struct {
		Deployments []CuratedRuleSetDeployment `json:"curatedRuleSetDeployments"`
	}
	sub := "curatedRuleSetCategories/-/curatedRuleSets/-/curatedRuleSetDeployments:batchUpdate"
	if err := c.post(ctx, c.resourcePath(sub, false), body, &resp); err != nil {
		return nil, err
	}
	return resp.Deployments, nil
}

// EnableCuratedRuleSet enables (enabled=true) or disables (enabled=false) a
// curated rule-set deployment. Convenience over UpdateCuratedRuleSetDeployment.
func (c *Client) EnableCuratedRuleSet(ctx context.Context, categoryID, ruleSetID, precision string, enabled bool) (*CuratedRuleSetDeployment, error) {
	return c.UpdateCuratedRuleSetDeployment(ctx, categoryID, ruleSetID, precision, CuratedDeploymentUpdate{Enabled: &enabled})
}

// SetCuratedRuleSetAlerting turns alert generation on/off for a curated rule-set
// deployment. Convenience over UpdateCuratedRuleSetDeployment.
func (c *Client) SetCuratedRuleSetAlerting(ctx context.Context, categoryID, ruleSetID, precision string, alerting bool) (*CuratedRuleSetDeployment, error) {
	return c.UpdateCuratedRuleSetDeployment(ctx, categoryID, ruleSetID, precision, CuratedDeploymentUpdate{Alerting: &alerting})
}

// ParseCuratedDeploymentName splits a deployment resource name into its category,
// rule-set, and precision segments. It accepts a full instance-prefixed name or
// the relative form. Returns an error if the expected segments are absent.
func ParseCuratedDeploymentName(name string) (categoryID, ruleSetID, precision string, err error) {
	categoryID = segmentAfter(name, "curatedRuleSetCategories")
	ruleSetID = segmentAfter(name, "curatedRuleSets")
	precision = segmentAfter(name, "curatedRuleSetDeployments")
	if categoryID == "" || ruleSetID == "" || precision == "" {
		return "", "", "", fmt.Errorf("chronicle: not a curated deployment name: %q", name)
	}
	return categoryID, ruleSetID, precision, nil
}

// segmentAfter returns the path segment immediately following the marker segment,
// or "" if the marker is absent or terminal.
func segmentAfter(path, marker string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if s == marker && i+1 < len(segs) {
			return segs[i+1]
		}
	}
	return ""
}
