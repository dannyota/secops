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
