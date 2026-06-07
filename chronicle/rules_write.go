package chronicle

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// Rule lifecycle writes — versioned updates, deletes, deployment toggles, and
// revision listing — mirroring the official wrapper's rule.py (update_rule,
// delete_rule, get_rule_deployment, enable_rule, set_rule_alerting). All
// instance URLs use the project ID form (numeric=false), matching the wrapper.

// UpdateRule replaces a rule's YARA-L source, creating a new rule version. It
// maps to PATCH rules/{ruleID} with updateMask=text, exactly as the wrapper's
// update_rule does. The returned *Rule reflects the new version (its Name
// carries the new @v_... suffix and a fresh Etag).
//
// etag, when non-empty, is round-tripped in the request body for optimistic
// concurrency: the server rejects the patch with a 409/aborted *APIError if the
// stored rule has moved on. Pass "" to skip the check.
//
// DEVIATION: the official wrapper sends no etag on update_rule, so a concurrent
// UI edit is silently clobbered. We honor the etag the API supports
// (resource-level optimistic concurrency) per the repo's etag house rule.
func (c *Client) UpdateRule(ctx context.Context, ruleID, text, etag string) (*Rule, error) {
	body := struct {
		Text string `json:"text"`
		Etag string `json:"etag,omitempty"`
	}{Text: text, Etag: etag}

	q := url.Values{"updateMask": {"text"}}
	var r Rule
	if err := c.patch(ctx, c.resourcePath("rules/"+ruleID, false), body, &r, withQuery(q)); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRule deletes a rule by ID. With force=true the rule is removed even if
// it has associated retrohunts (DELETE rules/{ruleID}?force=true), matching the
// wrapper's delete_rule. The endpoint returns an empty body on success.
func (c *Client) DeleteRule(ctx context.Context, ruleID string, force bool) error {
	var opts []requestOption
	if force {
		opts = append(opts, withQuery(url.Values{"force": {"true"}}))
	}
	return c.do(ctx, http.MethodDelete, c.resourcePath("rules/"+ruleID, false), nil, nil, opts...)
}

// GetRuleDeployment fetches a rule's current deployment (run/alert) state via
// GET rules/{ruleID}/deployment, mirroring the wrapper's get_rule_deployment.
// ruleID may carry a version suffix; without one the latest version is used.
func (c *Client) GetRuleDeployment(ctx context.Context, ruleID string) (*RuleDeployment, error) {
	var dep RuleDeployment
	if err := c.get(ctx, c.resourcePath("rules/"+ruleID+"/deployment", false), &dep); err != nil {
		return nil, err
	}
	return &dep, nil
}

// ListRuleRevisions returns every stored version of a rule, newest-to-oldest as
// the API orders them. It maps to the v1alpha rules.listRevisions method
// (GET rules/{ruleID}:listRevisions); the response key is "rules" and the
// owning ruleID must NOT include a version suffix.
//
// DEVIATION: the official wrapper has no rule-revisions helper — it only exposes
// view=REVISION_METADATA_ONLY on list_rules across all rules. We surface the
// per-rule revision history the API already provides, with full rule text per
// version (each element's Name carries its @v_... version id).
func (c *Client) ListRuleRevisions(ctx context.Context, ruleID string) ([]Rule, error) {
	// Strip any @v_... suffix: listRevisions rejects versioned ids.
	if i := strings.Index(ruleID, "@"); i >= 0 {
		ruleID = ruleID[:i]
	}
	path := c.resourcePath("rules/"+ruleID, false) + ":listRevisions"

	var all []Rule
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Rules         []Rule `json:"rules"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, path, &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Rules...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// EnableRule enables (enabled=true) or disables (enabled=false) a rule by
// patching its deployment. Convenience over UpdateRuleDeployment, matching the
// wrapper's enable_rule.
func (c *Client) EnableRule(ctx context.Context, ruleID string, enabled bool) (*RuleDeployment, error) {
	return c.UpdateRuleDeployment(ctx, ruleID, RuleDeploymentUpdate{Enabled: &enabled})
}

// SetRuleAlerting turns alert generation on (alerting=true) or off
// (alerting=false) for a rule's deployment. Convenience over
// UpdateRuleDeployment, matching the wrapper's set_rule_alerting.
func (c *Client) SetRuleAlerting(ctx context.Context, ruleID string, alerting bool) (*RuleDeployment, error) {
	return c.UpdateRuleDeployment(ctx, ruleID, RuleDeploymentUpdate{Alerting: &alerting})
}

// ArchiveRule archives (archived=true) or unarchives (archived=false) a rule's
// deployment. The server rejects `archived` combined with any other field
// ("cannot set archived to true if any other update_mask fields are set") and
// disables the rule on archive itself, so this sends `archived` ALONE.
// Convenience over UpdateRuleDeployment.
func (c *Client) ArchiveRule(ctx context.Context, ruleID string, archived bool) (*RuleDeployment, error) {
	return c.UpdateRuleDeployment(ctx, ruleID, RuleDeploymentUpdate{Archived: &archived})
}
