package soar

// Tier: MODERN — the enrichment agent on the SOAR host. The v1alpha docs file
// this resource under the chronicle instance path, but integration-flavored
// surfaces routinely answer on the SOAR host instead (the two-host rule); the
// CLI tries chronicle first and falls back here.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// FetchAlertData returns a SIEM alert's enrichment context
// (enrichmentAgent:fetchAlertData). Read-only.
func (c *Client) FetchAlertData(ctx context.Context, siemAlertID string) (json.RawMessage, error) {
	return c.enrichmentAgentGet(ctx, "fetchAlertData", siemAlertID)
}

// FetchAlertActions lists the integration actions executable against the
// alert's entities (enrichmentAgent:fetchActions). Read-only.
func (c *Client) FetchAlertActions(ctx context.Context, siemAlertID string) (json.RawMessage, error) {
	return c.enrichmentAgentGet(ctx, "fetchActions", siemAlertID)
}

func (c *Client) enrichmentAgentGet(ctx context.Context, verb, siemAlertID string) (json.RawMessage, error) {
	if strings.TrimSpace(siemAlertID) == "" {
		return nil, fmt.Errorf("soar: siemAlertID is required")
	}
	q := url.Values{"siemAlertId": {siemAlertID}}
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "enrichmentAgent:"+verb, nil, &out, transport.Query(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// ExecuteAlertActions runs a batch of enrichment-agent actions
// (enrichmentAgent:executeActions). body is the documented request
// ({siemAlertId, actions[]}). LIVE MUTATION — callers gate it.
func (c *Client) ExecuteAlertActions(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "enrichmentAgent:executeActions", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
