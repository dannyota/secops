// LEGACY tier: the Siemplify external API (/api/external/v1) Playbooks surface.
//
// Playbooks (workflows) are the automation graphs SOAR runs against cases and
// alerts. These methods cover the workflow-definition, debug/simulation, and
// pending-step operations not already implemented elsewhere in the package.
//
// As with the rest of the legacy tier, shapes are the deeply-nested,
// schema-unstable legacy payloads: reads return json.RawMessage and writes take
// a freeform body (the caller supplies/decodes only the fields it needs). All
// methods speak the AppKey-authenticated external API via c.t.External.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// PlaybookXCheckNameInDifferentEnvironments checks whether a workflow name is
// available across the given environments. body is the freeform legacy payload.
func (c *Client) PlaybookXCheckNameInDifferentEnvironments(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/CheckWorkflowNameInDifferentEnvironments", body)
}

// PlaybookXDuplicateNestedWorkflows duplicates a set of nested workflows. body
// is the freeform legacy payload. LIVE MUTATION.
func (c *Client) PlaybookXDuplicateNestedWorkflows(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/DuplicateNestedWorkflows", body)
}

// PlaybookXExecuteStep executes a single workflow step. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) PlaybookXExecuteStep(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/ExecuteStep", body)
}

// PlaybookXGetActionResultsOfWFId returns the action results produced by a
// workflow instance, by its (integer) workflow-instance id.
func (c *Client) PlaybookXGetActionResultsOfWFId(ctx context.Context, wfInstanceID int) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetActionResultsOfWFId/"+strconv.Itoa(wfInstanceID))
}

// PlaybookXGetCaseEntities returns the entities of a case, by its (integer)
// case id, in the playbook context.
func (c *Client) PlaybookXGetCaseEntities(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetCaseEntities/"+strconv.Itoa(caseID))
}

// PlaybookXGetContextGroupByKey returns the workflow context group for a key.
func (c *Client) PlaybookXGetContextGroupByKey(ctx context.Context, key string) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetContextGroupByKey/"+url.PathEscape(key))
}

// PlaybookXGetDebugStepCaseData returns the case data backing a debug step. body
// is the freeform legacy payload.
func (c *Client) PlaybookXGetDebugStepCaseData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetDebugStepCaseData", body)
}

// PlaybookXGetOverviewTemplateByRequest returns an overview template matching the
// request. body is the freeform legacy payload.
func (c *Client) PlaybookXGetOverviewTemplateByRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetOverviewTemplate", body)
}

// PlaybookXGetOverviewTemplate returns one overview template by its identifier.
func (c *Client) PlaybookXGetOverviewTemplate(ctx context.Context, templateIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetOverviewTemplate/"+url.PathEscape(templateIdentifier))
}

// PlaybookXGetPendingStep returns the details of a pending workflow step. body is
// the freeform legacy payload.
func (c *Client) PlaybookXGetPendingStep(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetPendingStep", body)
}

// PlaybookXGetPendingStepsCountForUser returns the number of pending workflow
// steps assigned to the current user.
func (c *Client) PlaybookXGetPendingStepsCountForUser(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetPendingStepsCountForUser")
}

// PlaybookXGetPendingStepsUserRelated returns the pending workflow steps related
// to the current user.
func (c *Client) PlaybookXGetPendingStepsUserRelated(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetPendingStepsUserRelated")
}

// PlaybookXGetSimulationEnrichment returns enrichment data for a playbook
// simulation. body is the freeform legacy payload.
func (c *Client) PlaybookXGetSimulationEnrichment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetPlaybookSimulationEnrichment", body)
}

// PlaybookXGetStatsMap returns the playbook statistics map. body is the freeform
// legacy payload.
func (c *Client) PlaybookXGetStatsMap(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetPlaybookStatsMap", body)
}

// WorkflowStatus is the status of a workflow instance (playbook run). The integer
// values are the server's WorkflowInstanceStatusEnum — sourced from the swagger
// schema description.
type WorkflowStatus int

const (
	WorkflowFaulted           WorkflowStatus = 0
	WorkflowInProgress        WorkflowStatus = 1
	WorkflowCompleted         WorkflowStatus = 2
	WorkflowPendingUserInput  WorkflowStatus = 3
	WorkflowPendingPrevSteps  WorkflowStatus = 4
	WorkflowStarted           WorkflowStatus = 5
	WorkflowFaultedAndSkipped WorkflowStatus = 6
	WorkflowHandledTimedout   WorkflowStatus = 7
)

func (s WorkflowStatus) String() string {
	switch s {
	case WorkflowFaulted:
		return "Faulted"
	case WorkflowInProgress:
		return "InProgress"
	case WorkflowCompleted:
		return "Completed"
	case WorkflowPendingUserInput:
		return "PendingUserInput"
	case WorkflowPendingPrevSteps:
		return "PendingPreviousSteps"
	case WorkflowStarted:
		return "Started"
	case WorkflowFaultedAndSkipped:
		return "FaultedAndSkipped"
	case WorkflowHandledTimedout:
		return "HandledTimedout"
	default:
		return "WorkflowStatus(" + strconv.Itoa(int(s)) + ")"
	}
}
