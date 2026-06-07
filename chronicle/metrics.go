package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Custom SOC metrics (metricDefinitions): a MetricDefinition specifies an
// aggregated metric Google computes from logs, with its textDefinition written in
// YARA-L 2.0. The resource id IS the display name. textDefinition is Immutable
// after create; patch updates ONLY the state (enable/disable). There is no delete
// API. All endpoints use the project ID (string) form (numeric=false), matching
// the other instance-scoped SIEM resources.

// MetricDefinitionState is the lifecycle state of a metric definition.
type MetricDefinitionState string

const (
	// MetricEnabled metrics can be used in other Google SecOps features.
	MetricEnabled MetricDefinitionState = "ENABLED"
	// MetricDisabled metrics cannot be used until re-enabled.
	MetricDisabled MetricDefinitionState = "DISABLED"
	// MetricPaused is set only by Google (a computation problem) and cannot be
	// requested by a client; it appears on reads and is left for Google to clear.
	MetricPaused MetricDefinitionState = "PAUSED"
)

// MetricDefinition is a custom SOC metric. Output-only fields (description,
// author, timestamps, the extracted match/outcome variables) are decoded for
// display but never sent on write — they are derived by the server from
// textDefinition.
type MetricDefinition struct {
	Name              string                `json:"name,omitempty"`
	Description       string                `json:"description,omitempty"`    // output only (extracted)
	TextDefinition    string                `json:"textDefinition,omitempty"` // required, immutable
	State             MetricDefinitionState `json:"state,omitempty"`
	Author            string                `json:"author,omitempty"`            // output only
	CreateTime        string                `json:"createTime,omitempty"`        // output only
	UpdateTime        string                `json:"updateTime,omitempty"`        // output only
	LastUpdater       string                `json:"lastUpdater,omitempty"`       // output only
	MatchVariables    []string              `json:"matchVariables,omitempty"`    // output only (extracted)
	MatchWindowLength string                `json:"matchWindowLength,omitempty"` // output only (extracted)
	OutcomeVariables  []string              `json:"outcomeVariables,omitempty"`  // output only (extracted)
	Raw               json.RawMessage       `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (m *MetricDefinition) UnmarshalJSON(data []byte) error {
	type alias MetricDefinition
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = MetricDefinition(a)
	m.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the metric's Name (its id is the
// display name), the identifier the Get/Patch paths take.
func (m *MetricDefinition) ID() string {
	if m == nil {
		return ""
	}
	return lastSegment(m.Name)
}

// ListMetricDefinitions returns every metric definition in the instance,
// paginating over nextPageToken.
func (c *Client) ListMetricDefinitions(ctx context.Context) ([]MetricDefinition, error) {
	var all []MetricDefinition
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}} // server caps metricDefinitions.list at 100
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			MetricDefinitions []MetricDefinition `json:"metricDefinitions"`
			NextPageToken     string             `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("metricDefinitions", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.MetricDefinitions...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetMetricDefinition fetches a single metric definition by its <id> segment (the
// id is the display name).
func (c *Client) GetMetricDefinition(ctx context.Context, id string) (*MetricDefinition, error) {
	var m MetricDefinition
	if err := c.get(ctx, c.resourcePath("metricDefinitions/"+url.PathEscape(lastSegment(id)), false), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMetricDefinition creates a metric definition with the given id (which
// becomes its display name; must match [a-z][_a-z0-9]{0,61}[a-z0-9]) and YARA-L
// textDefinition. An empty state lets the server default to ENABLED.
func (c *Client) CreateMetricDefinition(ctx context.Context, id, textDefinition string, state MetricDefinitionState) (*MetricDefinition, error) {
	body := struct {
		TextDefinition string                `json:"textDefinition"`
		State          MetricDefinitionState `json:"state,omitempty"`
	}{TextDefinition: textDefinition, State: state}

	q := url.Values{"metricDefinitionId": {lastSegment(id)}}
	var m MetricDefinition
	if err := c.post(ctx, c.resourcePath("metricDefinitions", false), body, &m, withQuery(q)); err != nil {
		return nil, err
	}
	return &m, nil
}

// SetMetricDefinitionState updates a metric definition's state (the only mutable
// field; textDefinition is immutable). PATCH {name}?updateMask=state.
func (c *Client) SetMetricDefinitionState(ctx context.Context, id string, state MetricDefinitionState) (*MetricDefinition, error) {
	body := struct {
		State MetricDefinitionState `json:"state"`
	}{State: state}
	q := url.Values{"updateMask": {"state"}}
	var m MetricDefinition
	if err := c.patch(ctx, c.resourcePath("metricDefinitions/"+url.PathEscape(lastSegment(id)), false), body, &m, withQuery(q)); err != nil {
		return nil, err
	}
	return &m, nil
}
