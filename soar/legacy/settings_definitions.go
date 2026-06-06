// LEGACY tier: Siemplify external API (/api/external/v1) DEFINITION settings —
// SLA definitions, case stages, tag definitions, and root-cause options. These
// are the reference data playbooks and analysts pick from. Config-as-code.
// Reads return RawJSON; writes take a freeform body.
package legacy

import "context"

// --- SLA definitions ---

// SLA enum codings are the SERVER's integers, sourced from the swagger schema
// `description` fields (NOT the `enum`/`x-enumNames` lists, which carry only the
// bare numbers). They are not contiguous or alphabetical — do not infer them.

// SlaProviderType is an SLA's subject type (ApiSlaProviderTypeEnum).
type SlaProviderType int

const (
	SlaAlertRuleGenerator SlaProviderType = 2
	SlaCaseStage          SlaProviderType = 3
	SlaCasePriority       SlaProviderType = 4
	SlaAlertPriority      SlaProviderType = 5
)

// SlaPeriodType is an SLA period unit (ApiPeriodTypeEnum).
type SlaPeriodType int

const (
	SlaMinutes SlaPeriodType = 0
	SlaHours   SlaPeriodType = 1
	SlaDays    SlaPeriodType = 2
	SlaSeconds SlaPeriodType = 3
)

// SlaAlertType scopes an SLA to all alerts or specific ones (ApiSlaAlertType).
type SlaAlertType int

const (
	SlaAllAlerts      SlaAlertType = 0
	SlaSpecificAlerts SlaAlertType = 1
)

// SlaDefinition is the typed body for an SLA definition (ApiSlaDefinition). For a
// CasePriority SLA the server stores Value as a JSON-array string (e.g. `["High"]`,
// the v1alpha slaTypeValue form), so set Value that way for a clean read round-trip;
// Values is the plain array.
type SlaDefinition struct {
	ID                 int             `json:"id,omitempty"`
	ValueType          SlaProviderType `json:"valueType"`
	Value              string          `json:"value"`
	Values             []string        `json:"values"`
	SlaPeriod          int             `json:"slaPeriod"`
	SlaPeriodType      SlaPeriodType   `json:"slaPeriodType"`
	CriticalPeriod     int             `json:"criticalPeriod"`
	CriticalPeriodType SlaPeriodType   `json:"criticalPeriodType"`
	AlertType          SlaAlertType    `json:"alertType"`
	Environments       []string        `json:"environments"`
}

// GetSlaDefinitionsRecords returns every SLA definition.
func (c *Client) GetSlaDefinitionsRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetSlaDefinitionsRecords")
}

// AddSlaDefinition is the typed form of AddSlaDefinitionsRecord. LIVE MUTATION.
func (c *Client) AddSlaDefinition(ctx context.Context, def SlaDefinition) (RawJSON, error) {
	if def.Values == nil {
		def.Values = []string{}
	}
	if def.Environments == nil {
		def.Environments = []string{}
	}
	return c.AddSlaDefinitionsRecord(ctx, def)
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

// RootCauseClose is a close root-cause option (a GetRootCauseCloseRecords record and
// the AddOrUpdateRootCauseClose body). ForCloseReason is the close reason this root
// cause is offered under — the SAME enum as BulkCloseCases (CloseReason: Malicious=0,
// …), carried here in the `forCloseReason` field. (The v1alpha twin is
// caseCloseDefinitions, which expresses the same link as a string closeReason.)
type RootCauseClose struct {
	ID             int         `json:"id,omitempty"`
	RootCause      string      `json:"rootCause"`
	ForCloseReason CloseReason `json:"forCloseReason"`
}

// GetRootCauseCloseRecords returns the close root-cause options.
func (c *Client) GetRootCauseCloseRecords(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetRootCauseCloseRecords")
}

// AddRootCauseClose is the typed form of AddOrUpdateRootCauseClose. LIVE MUTATION.
func (c *Client) AddRootCauseClose(ctx context.Context, rc RootCauseClose) (RawJSON, error) {
	return c.AddOrUpdateRootCauseClose(ctx, rc)
}

// AddOrUpdateRootCauseClose creates or updates a root-cause option. LIVE MUTATION.
func (c *Client) AddOrUpdateRootCauseClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateRootCauseClose", body)
}

// RemoveRootCauseClose deletes root-cause options. LIVE MUTATION.
func (c *Client) RemoveRootCauseClose(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/RemoveRootCauseClose", body)
}
