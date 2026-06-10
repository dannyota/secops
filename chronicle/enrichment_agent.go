package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// The enrichment agent (`{instance}/enrichmentAgent`) operates on a SIEM alert
// BEFORE (or without) case grouping: fetch the alert's investigation context,
// list the integration actions executable against its entities, and execute a
// batch of them. The pre-case half of alert triage; the in-case half is
// `soar case run-action`.

// FetchAlertData returns a SIEM alert's enrichment context — the minimal
// threat-indicator view, involved entities, mapped events, and comments
// (enrichmentAgent:fetchAlertData). Read-only.
func (c *Client) FetchAlertData(ctx context.Context, siemAlertID string) (json.RawMessage, error) {
	return c.enrichmentAgentGet(ctx, "fetchAlertData", siemAlertID)
}

// FetchAlertActions lists every integration action executable against the
// alert's entities, grouped per integration instance
// (enrichmentAgent:fetchActions). Read-only.
func (c *Client) FetchAlertActions(ctx context.Context, siemAlertID string) (json.RawMessage, error) {
	return c.enrichmentAgentGet(ctx, "fetchActions", siemAlertID)
}

func (c *Client) enrichmentAgentGet(ctx context.Context, verb, siemAlertID string) (json.RawMessage, error) {
	if strings.TrimSpace(siemAlertID) == "" {
		return nil, fmt.Errorf("chronicle: siemAlertID is required")
	}
	q := url.Values{"siemAlertId": {siemAlertID}}
	var out json.RawMessage
	path := c.resourcePath("enrichmentAgent:"+verb, false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// EnrichmentAction is one action of an ExecuteAlertActions batch
// (ExecuteActionRequest): which integration action to run, on which entities,
// with which parameters.
type EnrichmentAction struct {
	TargetEntities      []string          `json:"targetEntities"`
	Parameters          map[string]string `json:"parameters,omitempty"`
	DisplayName         string            `json:"displayName"`
	Integration         string            `json:"integration"`
	IntegrationInstance string            `json:"integrationInstance"`
}

// ExecuteAlertActions runs a batch of integration actions against a SIEM
// alert's entities (enrichmentAgent:executeActions) and returns the per-action
// results (status, message, resultJson, …). LIVE MUTATION — the actions run
// for real; callers gate this behind the standard guard.
func (c *Client) ExecuteAlertActions(ctx context.Context, siemAlertID string, actions []EnrichmentAction) (json.RawMessage, error) {
	if strings.TrimSpace(siemAlertID) == "" {
		return nil, fmt.Errorf("chronicle: siemAlertID is required")
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("chronicle: at least one action is required")
	}
	body := struct {
		SiemAlertID string             `json:"siemAlertId"`
		Actions     []EnrichmentAction `json:"actions"`
	}{SiemAlertID: siemAlertID, Actions: actions}
	var out json.RawMessage
	if err := c.post(ctx, c.resourcePath("enrichmentAgent:executeActions", false), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
