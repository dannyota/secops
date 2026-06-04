package legacy

// LEGACY tier — Siemplify external API (/api/external/v1) for playbook export.
//
// These endpoints predate the v1alpha playbook surface and return the full,
// freeform playbook definition. Callers persist the raw JSON verbatim (it is the
// canonical export format), so this file returns json.RawMessage rather than
// imposing a typed schema on a payload that exists to be round-tripped.

import (
	"context"
	"encoding/json"
)

// ListEnabledPlaybooks returns the enabled playbook "WF cards" (the lightweight
// catalog entries used to drive an export). The body is the raw response JSON.
//
// POST /playbooks/GetEnabledWFCards with an empty object body.
func (c *Client) ListEnabledPlaybooks(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	// DEVIATION: this endpoint requires a body; an empty JSON object satisfies it.
	if err := c.t.External(ctx, "POST", "/playbooks/GetEnabledWFCards", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExportPlaybook returns the full playbook definition (workflow plus all blocks)
// for the given identifier, as raw JSON to persist verbatim.
//
// GET /playbooks/ExportWorkflowWithBlocksByIdentifier/<identifier>.
func (c *Client) ExportPlaybook(ctx context.Context, identifier string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.External(ctx, "GET", "/playbooks/ExportWorkflowWithBlocksByIdentifier/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
