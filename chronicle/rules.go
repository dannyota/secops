package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Rules use the project ID (string) form in their resource path (numeric=false),
// matching the legacy tool, whose SDK builds the instance from the string
// project_id. See resource.go for why the form is explicit per endpoint.

// Severity is a rule's severity classification.
type Severity struct {
	DisplayName string `json:"displayName,omitempty"`
}

// UnmarshalJSON accepts severity in either shape the SecOps API uses: a JSON
// object {"displayName": "High"} (custom rules, deployments) or a bare JSON
// string "HIGH" (curated rule sets and featured content rules). Without this,
// the strict decode in Client.do would abort the whole list call on any
// instance whose curated/featured rules report a string severity.
//
// DEVIATION: the official Python wrapper has no single severity type; each call
// site re-implements the `isinstance(dict) ? ["displayName"] : value` dance. We
// absorb both shapes once, here, so every consumer reads .DisplayName.
func (s *Severity) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, &s.DisplayName)
	}
	type alias Severity // avoid recursing into this method
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*s = Severity(a)
	return nil
}

// Rule is a user-authored (custom) YARA-L detection rule.
//
// The resource Name is projects/.../rules/ru_xxxxxxxx; the ruleID used by the
// other methods is the final path segment (see RuleID).
type Rule struct {
	Name                  string    `json:"name,omitempty"`
	Text                  string    `json:"text,omitempty"`
	Etag                  string    `json:"etag,omitempty"`
	Type                  string    `json:"type,omitempty"`
	DisplayName           string    `json:"displayName,omitempty"`
	Severity              *Severity `json:"severity,omitempty"`
	AllowedRunFrequencies []string  `json:"allowedRunFrequencies,omitempty"`
	TimeWindowDuration    string    `json:"timeWindowDuration,omitempty"`
}

// RuleID returns the trailing ru_xxxxxxxx segment of the rule's resource Name,
// the identifier the API's GetRule/UpdateRuleDeployment paths expect.
func (r *Rule) RuleID() string {
	if r == nil || r.Name == "" {
		return ""
	}
	if i := strings.LastIndex(r.Name, "/rules/"); i >= 0 {
		return r.Name[i+len("/rules/"):]
	}
	return r.Name[strings.LastIndex(r.Name, "/")+1:]
}

// ListRules returns all custom detection rules (FULL view, so rule text is
// populated without a per-rule GetRule call).
//
// DEVIATION: we request the FULL view explicitly. The legacy tool fell back to
// GetRule per rule because the default BASIC view omits text; FULL avoids the
// N+1 round-trips entirely.
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var all []Rule
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}, "view": {"FULL"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Rules         []Rule `json:"rules"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("rules", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Rules...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetRule fetches a single rule by ID. ruleID is the ru_xxxxxxxx segment and
// may carry a version suffix ("ru_xxx@v_<sec>_<nano>"); without one the latest
// version is returned.
func (c *Client) GetRule(ctx context.Context, ruleID string) (*Rule, error) {
	var r Rule
	if err := c.get(ctx, c.resourcePath("rules/"+ruleID, false), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateRule creates a new detection rule from YARA-L source text.
func (c *Client) CreateRule(ctx context.Context, text string) (*Rule, error) {
	body := struct {
		Text string `json:"text"`
	}{Text: text}
	var r Rule
	if err := c.post(ctx, c.resourcePath("rules", false), body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ruleDiagnostic is one compilation diagnostic from verifyRuleText.
type ruleDiagnostic struct {
	Message  string         `json:"message,omitempty"`
	Severity string         `json:"severity,omitempty"`
	Position map[string]int `json:"position,omitempty"`
}

// RuleValidation is the distilled result of a verifyRuleText call.
type RuleValidation struct {
	Success bool   // true when the rule compiles (no ERROR diagnostics)
	Message string // first error message, empty on success
}

// ValidateRule checks YARA-L source against the API's verifyRuleText endpoint
// without creating a rule.
//
// DEVIATION: rather than trusting only the server's "success" flag, we treat an
// empty (or all-non-error) diagnostics list as success. This is more robust to
// the flag being absent and matches the task contract ("empty/blank error list
// -> Success=true").
func (c *Client) ValidateRule(ctx context.Context, text string) (*RuleValidation, error) {
	// verifyRuleText is an RPC-style method on the instance:
	// {instance}:verifyRuleText (no path separator, project ID form).
	path := c.instancePath(false) + ":verifyRuleText"
	body := struct {
		RuleText string `json:"ruleText"`
	}{RuleText: strings.Trim(text, "` \n\t\r")}

	var resp struct {
		Success                bool             `json:"success"`
		CompilationDiagnostics []ruleDiagnostic `json:"compilationDiagnostics"`
		CompatibilityVersions  []string         `json:"compatibilityVersions"`
	}
	if err := c.post(ctx, path, body, &resp); err != nil {
		return nil, err
	}

	// An error-severity diagnostic means the rule does not compile.
	for _, d := range resp.CompilationDiagnostics {
		if strings.EqualFold(d.Severity, "ERROR") {
			return &RuleValidation{Success: false, Message: d.Message}, nil
		}
	}
	if !resp.Success && len(resp.CompilationDiagnostics) > 0 {
		// No ERROR severity but the server flagged failure: surface the first.
		return &RuleValidation{Success: false, Message: resp.CompilationDiagnostics[0].Message}, nil
	}
	return &RuleValidation{Success: true}, nil
}

// RuleDeployment is the deployment (run/alert) state of a rule.
//
// Name is the deployment resource, projects/.../rules/ru_xxx/deployment; the
// owning ruleID is the segment between /rules/ and /deployment.
type RuleDeployment struct {
	Name           string `json:"name,omitempty"`
	Enabled        bool   `json:"enabled,omitempty"`
	Alerting       bool   `json:"alerting,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
	RunFrequency   string `json:"runFrequency,omitempty"`
	ExecutionState string `json:"executionState,omitempty"`
}

// RuleID returns the ru_xxxxxxxx segment owning this deployment.
func (d *RuleDeployment) RuleID() string {
	if d == nil || d.Name == "" {
		return ""
	}
	s := d.Name
	if i := strings.Index(s, "/rules/"); i >= 0 {
		s = s[i+len("/rules/"):]
	}
	if i := strings.Index(s, "/deployment"); i >= 0 {
		s = s[:i]
	}
	return s
}

// ListRuleDeployments returns the deployment state for every rule in the
// instance.
func (c *Client) ListRuleDeployments(ctx context.Context) ([]RuleDeployment, error) {
	var all []RuleDeployment
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			RuleDeployments []RuleDeployment `json:"ruleDeployments"`
			NextPageToken   string           `json:"nextPageToken"`
		}
		// The collection lives under the wildcard rule: rules/-/deployments.
		if err := c.get(ctx, c.resourcePath("rules/-/deployments", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.RuleDeployments...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// RuleDeploymentUpdate is a partial update to a rule's deployment. Only the
// non-nil/non-empty fields are sent, and the updateMask is derived from them.
type RuleDeploymentUpdate struct {
	Enabled      *bool  // nil leaves enabled unchanged
	Alerting     *bool  // nil leaves alerting unchanged
	Archived     *bool  // nil leaves archive state unchanged. MUST be sent alone — the server rejects `archived` combined with any other field, and disables the rule on archive itself
	RunFrequency string // "" leaves run frequency unchanged ("LIVE"/"HOURLY"/"DAILY")
}

// UpdateRuleDeployment patches a rule's deployment, sending only the fields set
// on upd and an updateMask covering exactly those fields.
//
// DEVIATION: the update body is a typed struct with explicit JSON omission
// rather than a map[string]any assembled field-by-field; the updateMask is
// built from the same set so body and mask never drift.
func (c *Client) UpdateRuleDeployment(ctx context.Context, ruleID string, upd RuleDeploymentUpdate) (*RuleDeployment, error) {
	body := struct {
		Enabled      *bool  `json:"enabled,omitempty"`
		Alerting     *bool  `json:"alerting,omitempty"`
		Archived     *bool  `json:"archived,omitempty"`
		RunFrequency string `json:"runFrequency,omitempty"`
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
	if upd.Archived != nil {
		body.Archived = upd.Archived
		mask = append(mask, "archived")
	}
	if upd.RunFrequency != "" {
		body.RunFrequency = upd.RunFrequency
		mask = append(mask, "runFrequency")
	}
	if len(mask) == 0 {
		return nil, &APIError{
			Method: "PATCH",
			URL:    c.resourcePath("rules/"+ruleID+"/deployment", false),
			Status: 0,
			Body:   "no deployment fields provided to update",
		}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var dep RuleDeployment
	if err := c.patch(ctx, c.resourcePath("rules/"+ruleID+"/deployment", false), body, &dep, withQuery(q)); err != nil {
		return nil, err
	}
	return &dep, nil
}
