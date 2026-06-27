// MODERN. Alert-grouping rules and module settings on the v1alpha SOAR surface.
//
// Alert-grouping rules control how inbound alerts are coalesced into cases.
// Module settings are per-tenant configuration bags for SOAR modules; their
// properties are key/value pairs where every Value is a string even when it
// encodes an int or bool (the SOAR API returns all settings as strings).

package soar

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// AlertGroupingRule describes how inbound alerts are grouped into cases. Raw
// retains the full server object for fields not modeled here.
type AlertGroupingRule struct {
	Name         string          `json:"name,omitempty"`         // full resource name
	ID           string          `json:"-"`                      // server id (numeric or string in the payload)
	Category     string          `json:"category,omitempty"`     // rule category
	GroupingType string          `json:"groupingType,omitempty"` // how alerts are coalesced
	EntityType   []string        `json:"entityType,omitempty"`   // entity types the rule keys on
	Raw          json.RawMessage `json:"-"`                      // full server payload
}

// UnmarshalJSON keeps the typed fields and the complete payload in sync. The
// v1alpha payload returns id as a JSON number; an older shape used a string —
// accept either and normalize to a string.
func (r *AlertGroupingRule) UnmarshalJSON(data []byte) error {
	type alias AlertGroupingRule
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = AlertGroupingRule(a)
	var idh struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(data, &idh) == nil && len(idh.ID) > 0 {
		r.ID = string(bytes.Trim(idh.ID, `"`))
	}
	r.Raw = append(r.Raw[:0], data...)
	return nil
}

// ModuleSettingProperty is a single module setting. Value is always a string,
// even for ints or bools — callers parse it themselves. Raw retains the full
// server object.
type ModuleSettingProperty struct {
	Name string `json:"name,omitempty"`
	// DisplayName is the short, human property name (the resource name's last
	// segment, e.g. "TimeframeForGroupingInHours") that callers key on.
	DisplayName string `json:"displayName,omitempty"`
	// No omitempty: every property value is a string, so "" is a legitimate
	// value the batchUpdate write path must transmit, not drop.
	Value string          `json:"value"`
	Raw   json.RawMessage `json:"-"`
}

// ShortName returns the friendly property name: DisplayName when present, else
// the resource name's last path segment.
func (p ModuleSettingProperty) ShortName() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if i := strings.LastIndex(p.Name, "/"); i >= 0 {
		return p.Name[i+1:]
	}
	return p.Name
}

// UnmarshalJSON keeps the typed fields and the complete payload in sync.
func (p *ModuleSettingProperty) UnmarshalJSON(data []byte) error {
	type alias ModuleSettingProperty
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = ModuleSettingProperty(a)
	p.Raw = append(p.Raw[:0], data...)
	return nil
}

// alertGroupingRulesPage is one page of ListAlertGroupingRules.
type alertGroupingRulesPage struct {
	Items         []AlertGroupingRule `json:"alertGroupingRules"`
	NextPageToken string              `json:"nextPageToken"`
}

// moduleSettingPropertiesPage is one page of ListModuleSettingProperties. The
// v1alpha response returns the list under the canonical `items` field (it also
// echoes `properties`/`moduleSettingsProperties` with the same data).
type moduleSettingPropertiesPage struct {
	Items         []ModuleSettingProperty `json:"items"`
	NextPageToken string                  `json:"nextPageToken"`
}

// ListAlertGroupingRules returns every alert-grouping rule on the instance.
func (c *Client) ListAlertGroupingRules(ctx context.Context) ([]AlertGroupingRule, error) {
	var rules []AlertGroupingRule
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page alertGroupingRulesPage
		if err := c.t.V1Alpha(ctx, "GET", "alertGroupingRules", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		rules = append(rules, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// GetAlertGroupingRule fetches a single alert-grouping rule by id.
func (c *Client) GetAlertGroupingRule(ctx context.Context, id string) (*AlertGroupingRule, error) {
	var rule AlertGroupingRule
	if err := c.t.V1Alpha(ctx, "GET", "alertGroupingRules/"+id, nil, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// UpdateAlertGroupingRule applies a sparse update to a rule. body is the partial
// rule (struct or map); updateMask names the fields to write — pass it to avoid
// clobbering unset fields.
func (c *Client) UpdateAlertGroupingRule(ctx context.Context, id string, body any, updateMask ...string) (*AlertGroupingRule, error) {
	var opts []transport.Option
	if len(updateMask) > 0 {
		opts = append(opts, transport.UpdateMask(updateMask...))
	}
	var rule AlertGroupingRule
	if err := c.t.V1Alpha(ctx, "PATCH", "alertGroupingRules/"+id, body, &rule, opts...); err != nil {
		return nil, err
	}
	return &rule, nil
}

// CreateAlertGroupingRule creates an alert-grouping rule from body (the new rule
// as a struct or map). Completes the rule lifecycle (list/get/patch/delete) so a
// new rule can be pushed from git rather than hand-created in the UI first.
// LIVE MUTATION. (Modern SOAR v1alpha — may 500 intermittently.)
func (c *Client) CreateAlertGroupingRule(ctx context.Context, body any) (*AlertGroupingRule, error) {
	var rule AlertGroupingRule
	if err := c.t.V1Alpha(ctx, "POST", "alertGroupingRules", body, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// DeleteAlertGroupingRule deletes an alert-grouping rule by id — needed for the
// --prune side of a reconcile loop. LIVE MUTATION.
func (c *Client) DeleteAlertGroupingRule(ctx context.Context, id string) error {
	return c.t.V1Alpha(ctx, "DELETE", "alertGroupingRules/"+id, nil, nil)
}

// GetModuleSettings returns the raw settings object for a named module. The
// shape is module-specific, so it is returned undecoded.
func (c *Client) GetModuleSettings(ctx context.Context, name string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "moduleSettings/"+name, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ListModuleSettingProperties returns every key/value property of a module's
// settings. Every Value is a string regardless of its logical type.
func (c *Client) ListModuleSettingProperties(ctx context.Context, name string) ([]ModuleSettingProperty, error) {
	var props []ModuleSettingProperty
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page moduleSettingPropertiesPage
		if err := c.t.V1Alpha(ctx, "GET", "moduleSettings/"+name+"/properties", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		props = append(props, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return props, nil
}

// BatchUpdateModuleSettingProperties writes a batch of properties to a module's
// settings in one call. Set each property's Value as a string. The response
// shape is module-specific, so it is returned raw.
func (c *Client) BatchUpdateModuleSettingProperties(ctx context.Context, name string, props []ModuleSettingProperty) (json.RawMessage, error) {
	// The batchUpdate RPC takes a list of per-property requests, each wrapping
	// one property under "moduleSettingsProperty":
	//   {"requests":[{"moduleSettingsProperty":{"name":...,"value":...}}, ...]}
	type updateRequest struct {
		ModuleSettingsProperty ModuleSettingProperty `json:"moduleSettingsProperty"`
	}
	body := struct {
		Requests []updateRequest `json:"requests"`
	}{Requests: make([]updateRequest, len(props))}
	for i, p := range props {
		body.Requests[i].ModuleSettingsProperty = p
	}
	var raw json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "moduleSettings/"+name+"/properties:batchUpdate", body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
