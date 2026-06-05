// LEGACY tier: the Siemplify external API (/api/external/v1) Playbooks surface —
// run/rerun/debug and workflow-introspection operations: test cases, trigger
// tags, step instances, rerun/debug-run, and log-versioning.
//
// Reads return json.RawMessage and writes take a freeform body; all methods
// speak the AppKey-authenticated external API via c.t.External.
package legacy

import "context"

// PlaybookXGetTestCases returns the test cases available for a playbook. body is
// the freeform legacy payload.
func (c *Client) PlaybookXGetTestCases(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetTestCases", body)
}

// PlaybookXGetTriggerTags returns the trigger tags for a playbook. body is the
// freeform legacy payload.
func (c *Client) PlaybookXGetTriggerTags(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetTriggerTags", body)
}

// PlaybookXGetWorkflowStepInstance returns a single workflow step instance. body
// is the freeform legacy payload.
func (c *Client) PlaybookXGetWorkflowStepInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetWorkflowStepInstance", body)
}

// PlaybookXGetWorkflowsInvolvingAction returns the workflows that use a given
// action. body is the freeform legacy payload.
func (c *Client) PlaybookXGetWorkflowsInvolvingAction(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetWorkflowsInvolvingAction", body)
}

// PlaybookXRerunBlock reruns a workflow block. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) PlaybookXRerunBlock(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/RerunBlock", body)
}

// PlaybookXRerun reruns a playbook. body is the freeform legacy payload.
// LIVE MUTATION.
func (c *Client) PlaybookXRerun(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/RerunPlaybook", body)
}

// PlaybookXRunInDebug runs a playbook in debug mode. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) PlaybookXRunInDebug(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/RunPlaybookInDebug", body)
}

// PlaybookXSaveLogVersionOfWorkflowDefinitions saves a log version of the given
// workflow definitions. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) PlaybookXSaveLogVersionOfWorkflowDefinitions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/SaveLogVersionOfWorkflowDefinitions", body)
}

// PlaybookXGetActionWidgetTemplate returns the action-widget template.
func (c *Client) PlaybookXGetActionWidgetTemplate(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/action-widget-template")
}
