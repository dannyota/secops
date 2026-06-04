package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Curated content: vendor-managed rule-set categories, rule sets, their
// per-precision deployment state, and Content-Hub "featured" rules. Only the
// categories and rule-set raw collections require the project NUMBER
// (numeric=true); deployments and featured content rules use the project ID
// (numeric=false), matching the legacy tool's SDK calls. See resource.go.

// CuratedRuleSetCategory is a top-level grouping of curated rule sets
// (e.g. "Cloud Threats"). Name is the full resource name; its last path
// segment is the category ID used to list the category's rule sets.
type CuratedRuleSetCategory struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// CuratedRuleSet is a vendor-curated set of detections within a category.
// Precisions are the available precision tiers ("precise"/"broad") and
// Severity is the set's default severity, when the API reports one.
type CuratedRuleSet struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description,omitempty"`
	Precisions  []string  `json:"precisions,omitempty"`
	Severity    *Severity `json:"severity,omitempty"`
}

// CuratedRuleSetDeployment is the deployment state of a single rule set at a
// single precision. Name encodes category/set/precision; the last path segment
// is the precision. Enabled toggles the set; Alerting toggles alert emission.
type CuratedRuleSetDeployment struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Alerting bool   `json:"alerting"`
}

// FeaturedContentRule is a Content-Hub ("featured") curated rule. The model
// mirrors the fields the legacy pull consumed; richer freeform sub-objects
// (content metadata, curated rule content, rule-set linkage) are kept as raw
// JSON so callers can extract what they need without a brittle schema.
type FeaturedContentRule struct {
	Name                  string          `json:"name"`
	Type                  string          `json:"type,omitempty"`
	CategoryID            string          `json:"categoryId,omitempty"`
	RuleText              string          `json:"ruleText,omitempty"`
	RuleTextHidden        bool            `json:"ruleTextHidden,omitempty"`
	LiveStatusEnabled     bool            `json:"liveStatusEnabled,omitempty"`
	AlertingStatusEnabled bool            `json:"alertingStatusEnabled,omitempty"`
	NonUpgradable         bool            `json:"nonUpgradable,omitempty"`
	PrivateRule           bool            `json:"privateRule,omitempty"`
	Severity              *Severity       `json:"severity,omitempty"`
	ContentMetadata       json.RawMessage `json:"contentMetadata,omitempty"`
	CuratedRuleContent    json.RawMessage `json:"curatedRuleContent,omitempty"`
	RuleSet               json.RawMessage `json:"ruleSet,omitempty"`
}

// ListCuratedRuleSetCategories returns all curated rule-set categories.
func (c *Client) ListCuratedRuleSetCategories(ctx context.Context) ([]CuratedRuleSetCategory, error) {
	var all []CuratedRuleSetCategory
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Categories    []CuratedRuleSetCategory `json:"curatedRuleSetCategories"`
			NextPageToken string                   `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("curatedRuleSetCategories", true), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Categories...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ListCuratedRuleSets returns the curated rule sets within one category.
// categoryID is the last path segment of a CuratedRuleSetCategory.Name.
func (c *Client) ListCuratedRuleSets(ctx context.Context, categoryID string) ([]CuratedRuleSet, error) {
	var all []CuratedRuleSet
	sub := "curatedRuleSetCategories/" + categoryID + "/curatedRuleSets"
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			RuleSets      []CuratedRuleSet `json:"curatedRuleSets"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(sub, true), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.RuleSets...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ListCuratedRuleSetDeployments returns deployment state for every rule set at
// every precision, using the category/set wildcard collection.
//
// DEVIATION: the Python wrapper post-enriches each deployment with the parent
// set's displayName via a second full list call and supports only_enabled/
// only_alerting client-side filters. We return the raw deployments; callers
// join against ListCuratedRuleSets (the Name field carries the IDs) and filter
// themselves, keeping this method a thin, single-concern list.
func (c *Client) ListCuratedRuleSetDeployments(ctx context.Context) ([]CuratedRuleSetDeployment, error) {
	var all []CuratedRuleSetDeployment
	sub := "curatedRuleSetCategories/-/curatedRuleSets/-/curatedRuleSetDeployments"
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Deployments   []CuratedRuleSetDeployment `json:"curatedRuleSetDeployments"`
			NextPageToken string                     `json:"nextPageToken"`
		}
		// numeric=false: the legacy tool listed deployments through the SDK,
		// which uses the string project_id (unlike the raw categories/sets calls).
		if err := c.get(ctx, c.resourcePath(sub, false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Deployments...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ListFeaturedContentRules returns Content-Hub featured curated rules. A
// non-empty filter is passed through as the API "filter" expression (e.g.
// `category_name:"Cloud Threats"`, `rule_precision:"Precise"`, combinable with
// AND). Pagination is capped at 50 pages.
//
// DEVIATION: the wrapper's Python parameter is named filter_expression but the
// wire query key it sets is "filter" — we expose it directly as the filter
// string and send key "filter". The collection lives under contentHub/ (per the
// wrapper), not at the instance root.
func (c *Client) ListFeaturedContentRules(ctx context.Context, filter string) ([]FeaturedContentRule, error) {
	var all []FeaturedContentRule
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		if filter != "" {
			q.Set("filter", filter)
		}
		var resp struct {
			Rules         []FeaturedContentRule `json:"featuredContentRules"`
			NextPageToken string                `json:"nextPageToken"`
		}
		// numeric=false: featured content rules were listed via the SDK (string project_id).
		if err := c.get(ctx, c.resourcePath("contentHub/featuredContentRules", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Rules...)
		return resp.NextPageToken, nil
	})
	return all, err
}
