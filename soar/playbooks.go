package soar

import (
	"context"
	"encoding/json"
)

// GetWorkflowInstanceSummary returns a playbook run's workflow-instance summary —
// its steps and, when the body sets shouldFetchSteps, each step's execution
// result (including the failure/traceback of a failed step) — via the v1alpha
// SOAR-host legacyPlaybooks surface.
//
// body carries the run selectors: caseId (string), alertIdentifier, and
// definitionIdentifier (the playbook id); parentWorkflowInstanceId is only needed
// to address a nested-loop iteration. All three core selectors are required — the
// server returns a generic 500 (errorCode 2000) when one is missing, not a 4xx.
//
// This is the modern twin of the legacy external-API method (POST
// /cases/GetWorkflowInstanceSummary, AppKey); both serve the same shape. Per
// project direction the CLI prefers this v1alpha path and falls back to the legacy
// one (see internal/cli preferModern).
func (c *Client) GetWorkflowInstanceSummary(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetWorkflowInstanceSummary", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkflowFullInfo returns the full definition of a playbook by its identifier
// via the v1alpha SOAR-host path. The returned object is the same shape that
// SaveWorkflowDefinitions accepts as its body.
func (c *Client) GetWorkflowFullInfo(ctx context.Context, identifier string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacyPlaybooks:legacyGetWorkflowFullInfoByIdentifier/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveWorkflowDefinitions saves (creates or updates) a playbook definition via
// the v1alpha SOAR-host path. body is the full ApiWorkflowDefinitionDataModel.
// This mints a new version — there is no partial-update or toggle-only path.
func (c *Client) SaveWorkflowDefinitions(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacySaveWorkflowDefinitions", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DuplicateWorkflows duplicates one or more playbook definitions via the
// v1alpha SOAR-host path. body carries:
//
//	{"identifiers":["uuid",...],"priority":0,"categoryId":0,"environments":["Default Environment"]}
//
// priority and categoryId 0 = keep original value. The copy is auto-named
// "Copy of <original>". Returns {"payload":[...]} wrapping the full
// definitions of the created copies.
func (c *Client) DuplicateWorkflows(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyDuplicateWorkflows", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteWorkflows deletes one or more playbook definitions by their identifiers
// via the v1alpha SOAR-host legacyPlaybooks:legacyDeleteWorkflows surface.
// identifiers is the list of workflow definition UUIDs to delete.
func (c *Client) DeleteWorkflows(ctx context.Context, identifiers []string) (json.RawMessage, error) {
	body := map[string]any{"identifiers": identifiers}
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyDeleteWorkflows", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
