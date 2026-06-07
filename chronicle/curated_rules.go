package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// CuratedRule is one Google-managed (curated) rule — the individual rule behind a
// curated rule set, distinct from the rule SETS (ListCuratedRuleSets) and from the
// Content-Hub featured rules (ListFeaturedContentRules). Read-only: curated rules
// are managed by Google; an operator toggles them at the rule-SET deployment level
// (see UpdateCuratedRuleSetDeployment / BatchUpdateCuratedRuleSetDeployments). The
// stable framing is typed; the full object is kept in Raw.
type CuratedRule struct {
	Name        string          `json:"name"`        // full resource name (.../curatedRules/{ur_...})
	ID          string          `json:"-"`           // short id (last name segment), e.g. "ur_..."
	RuleID      string          `json:"ruleId"`      // server rule id, when distinct from the name segment
	DisplayName string          `json:"displayName"` // a.k.a. ruleName on some payloads
	Severity    *Severity       `json:"severity"`
	RuleType    string          `json:"ruleType"`
	Precision   string          `json:"precision"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields, derives the short ID from the resource
// name, and keeps the full server object in Raw.
func (r *CuratedRule) UnmarshalJSON(data []byte) error {
	type alias CuratedRule
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = CuratedRule(a)
	r.ID = lastSegment(r.Name)
	r.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListCuratedRules returns the curated (Google-managed) rules. Read-only.
//
// Endpoint: GET {instance}/curatedRules (project-ID form). Paginated.
func (c *Client) ListCuratedRules(ctx context.Context) ([]CuratedRule, error) {
	var all []CuratedRule
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			CuratedRules  []CuratedRule `json:"curatedRules"`
			NextPageToken string        `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("curatedRules", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.CuratedRules...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetCuratedRule fetches one curated rule by its short id (e.g. "ur_...") or full
// resource name. Read-only.
func (c *Client) GetCuratedRule(ctx context.Context, id string) (*CuratedRule, error) {
	var out CuratedRule
	if err := c.get(ctx, c.resourcePath("curatedRules/"+url.PathEscape(lastSegment(id)), false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
