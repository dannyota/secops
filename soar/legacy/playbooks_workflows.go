// LEGACY tier: the Siemplify external API (/api/external/v1) playbook *workflow
// definition* surface. (The external API calls playbooks "workflows".)
//
// This file is the definition-management slice: list/get/save/delete/duplicate/
// restore workflow definitions and export/import them — the config-as-code core.
// Category management lives in playbooks_categories.go; the v1alpha bridge for
// playbooks is in playbooks.go. Reads return RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/url"
)

// ListEnabledWorkflowCards returns summary cards for enabled workflows. body is
// the freeform filter payload.
func (c *Client) ListEnabledWorkflowCards(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetEnabledWFCards", body)
}

// GetWorkflowFullInfo returns the full definition of one workflow by its api
// workflow identifier.
func (c *Client) GetWorkflowFullInfo(ctx context.Context, apiWfIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetWorkflowFullInfoByIdentifier/"+url.PathEscape(apiWfIdentifier))
}

// GetWorkflowFullInfoWithEnvFilter is GetWorkflowFullInfo scoped to the caller's
// environment filter.
func (c *Client) GetWorkflowFullInfoWithEnvFilter(ctx context.Context, apiWfIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/GetWorkflowFullInfoWithEnvFilterByIdentifier/"+url.PathEscape(apiWfIdentifier))
}

// ExportWorkflowWithBlocks exports one workflow (with its nested blocks) by
// identifier — the portable definition for storing as code.
func (c *Client) ExportWorkflowWithBlocks(ctx context.Context, apiWfIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/playbooks/ExportWorkflowWithBlocksByIdentifier/"+url.PathEscape(apiWfIdentifier))
}

// GetWorkflowVersionLogs returns the version history of workflow definitions.
// body is the freeform selector payload.
func (c *Client) GetWorkflowVersionLogs(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/GetWorkFlowVersionLogs", body)
}

// SaveWorkflowDefinitions creates or updates workflow definitions. body is the
// freeform definitions payload. LIVE MUTATION — mints a new version.
func (c *Client) SaveWorkflowDefinitions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/SaveWorkflowDefinitions", body)
}

// RestoreWorkflowDefinition restores a workflow definition to a prior version.
// body carries the target. LIVE MUTATION.
func (c *Client) RestoreWorkflowDefinition(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/RestoreWorkflowDefinition", body)
}

// DeleteWorkflow deletes a single workflow definition. body carries its id.
// LIVE MUTATION; this cannot be undone.
func (c *Client) DeleteWorkflow(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/DeleteWorkflow", body)
}

// DeleteWorkflows deletes multiple workflow definitions. body carries the ids.
// LIVE MUTATION; this cannot be undone.
func (c *Client) DeleteWorkflows(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/DeleteWorkflows", body)
}

// CloneWorkflow creates an exact copy of a workflow definition. body carries the
// source definition. Per Google docs, Clone is "exact copy" while Duplicate is
// "template-based" — they share the same request/response shape. LIVE MUTATION.
func (c *Client) CloneWorkflow(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/CloneWorkflow", body)
}

// DuplicateWorkflow clones one workflow definition. body carries the source id.
// LIVE MUTATION.
func (c *Client) DuplicateWorkflow(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/DuplicateWorkflow", body)
}

// DuplicateWorkflows clones multiple workflow definitions. LIVE MUTATION.
func (c *Client) DuplicateWorkflows(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/DuplicateWorkflows", body)
}

// ExportPlaybookDefinitions exports a set of playbook definitions. body selects
// what to export; the result is the portable bundle for storing as code.
func (c *Client) ExportPlaybookDefinitions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/ExportDefinitions", body)
}

// ImportPlaybookDefinitions imports a previously exported definition bundle.
// LIVE MUTATION.
func (c *Client) ImportPlaybookDefinitions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/ImportDefinitions", body)
}

// AttachWorkflowToCase attaches a workflow to a case. body carries the case and
// workflow ids. LIVE MUTATION.
func (c *Client) AttachWorkflowToCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/AttacheWorkflowToCase", body)
}

// AttachNestedWorkflowToCase attaches a nested workflow (block) to a case.
// LIVE MUTATION.
func (c *Client) AttachNestedWorkflowToCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/AttacheNestedWorkflowToCase", body)
}
