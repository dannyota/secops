package soar

import (
	"context"
	"encoding/json"
)

// GetWorkflowInstanceCards returns the execution history cards for a playbook
// run (or all runs for a case/alert). The body carries selectors: caseId,
// alertIdentifier — the server returns a card per playbook execution. This is
// the v1alpha twin of the legacy external API's GetWorkflowInstancesCards.
func (c *Client) GetWorkflowInstanceCards(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetWorkflowInstancesCards", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkflowInstance returns the full step-by-step execution detail for one
// playbook run instance — each step's status, input/output, timing, and error
// detail. body carries: workflowInstanceId (the execution id from the cards).
func (c *Client) GetWorkflowInstance(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetWorkflowInstance", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEnvironmentActionDefinitions returns ALL action definitions for an
// environment in a single call — the all-in-one action palette (368 actions /
// 34 integrations on a typical tenant). body carries: environmentName.
func (c *Client) GetEnvironmentActionDefinitions(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacySoarSettings:legacyGetEnvironmentActionDefinitions", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetNestedPlaybooks returns the nested (block) playbooks available in the
// given environments, formatted as step definitions. body carries:
// environmentNames (string array).
func (c *Client) GetNestedPlaybooks(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetNestedPlaybooksByEnvironmentsAsSteps", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookCategories returns the playbook category list.
func (c *Client) GetPlaybookCategories(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacyPlaybooks:legacyGetWorkflowCategories", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookMenuCards returns the playbook list with environment filtering,
// as shown in the playbook manager sidebar. body carries: environmentNames
// (string array) and optional folderName.
func (c *Client) GetPlaybookMenuCards(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetWorkflowMenuCardsWithEnvFilter", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookFullInfoWithEnvFilter returns a playbook's full definition with
// environment filtering — the same shape as GetWorkflowFullInfo but scoped to
// the environments the caller has access to. body carries: identifier (string),
// environmentNames (string array).
func (c *Client) GetPlaybookFullInfoWithEnvFilter(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetWorkflowFullInfoWithEnvFilterByIdentifier", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookPermissionsOptions returns the permission model options for
// playbook access control. body carries the request parameters (e.g.
// workflowIdentifier).
func (c *Client) GetPlaybookPermissionsOptions(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyPermissionsOptions", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
