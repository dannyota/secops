// LEGACY tier: Siemplify external API (/api/external/v1) DEFINITION settings —
// SLA definitions, case stages, tag definitions, and root-cause options. These
// are the reference data playbooks and analysts pick from. Config-as-code.
// Reads return RawJSON; writes take a freeform body.
package legacy

import "context"

// --- SLA definitions ---

// GetSlaDefinitionsRecords returns every SLA definition.
func (c *Client) GetSlaDefinitionsRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetSlaDefinitionsRecords")
}

// GetSlaDefinitions returns filtered SLA definitions. body is the filter payload.
func (c *Client) GetSlaDefinitions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetSlaDefinitions", body)
}

// AddSlaDefinitionsRecord creates or updates an SLA definition. LIVE MUTATION.
func (c *Client) AddSlaDefinitionsRecord(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddSlaDefinitionsRecord", body)
}

// RemoveSlaDefinitionRecords deletes SLA definitions. LIVE MUTATION.
func (c *Client) RemoveSlaDefinitionRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveSlaDefinitionRecords", body)
}

// --- Case stages ---

// GetCaseStageDefinitionRecords returns the case-stage definitions. body is the
// freeform filter payload.
func (c *Client) GetCaseStageDefinitionRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetCaseStageDefinitionRecords", body)
}

// AddCaseStageDefinitionRecord creates or updates a case stage. LIVE MUTATION.
func (c *Client) AddCaseStageDefinitionRecord(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddCaseStageDefinitionRecord", body)
}

// RemoveCaseStageDefinitionRecords deletes case stages. LIVE MUTATION.
func (c *Client) RemoveCaseStageDefinitionRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveCaseStageDefinitionRecords", body)
}

// --- Tag definitions ---

// GetTagDefinitionNames returns the defined tag names.
func (c *Client) GetTagDefinitionNames(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/tag-definition/names")
}

// GetTagDefinitionsRecords returns the tag definitions. body is the filter payload.
func (c *Client) GetTagDefinitionsRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetTagDefinitionsRecords", body)
}

// AddTagDefinitionsRecords creates or updates tag definitions. LIVE MUTATION.
func (c *Client) AddTagDefinitionsRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddTagDefinitionsRecords", body)
}

// RemoveTagDefinitionRecords deletes tag definitions. LIVE MUTATION.
func (c *Client) RemoveTagDefinitionRecords(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveTagDefinitionRecords", body)
}

// --- Root-cause (close) options ---

// GetRootCauseCloseRecords returns the close root-cause options.
func (c *Client) GetRootCauseCloseRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetRootCauseCloseRecords")
}

// AddOrUpdateRootCauseClose creates or updates a root-cause option. LIVE MUTATION.
func (c *Client) AddOrUpdateRootCauseClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateRootCauseClose", body)
}

// RemoveRootCauseClose deletes root-cause options. LIVE MUTATION.
func (c *Client) RemoveRootCauseClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveRootCauseClose", body)
}
