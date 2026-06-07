// case_data_surfaces.go — MODERN v1alpha "Case Data" config-as-code surfaces on
// the SOAR host (siemplify-soar, AppKey): custom fields, calculated-field
// definitions (formula-driven derived fields), and property-schema definitions
// (display/grouping metadata). Full CRUD via the shared collection helpers in
// config_surfaces.go; bodies/records are raw JSON (shapes vary — see the v1alpha
// REST reference). Pre-GA surfaces.
//
// Dependency: a CalculatedFieldDefinition's targetField must be an Active
// Free-Text CustomField (e.g. "CaseCustom.<field>"), so create the field before
// the calc and delete the calc before the field.

package soar

import (
	"context"
	"encoding/json"
)

// CustomField writes/reads (modern v1alpha). type/scopes/name are immutable after
// create; a FREE_TEXT field needs no type_options. LIVE MUTATIONS for the writes.
func (c *Client) ListCustomFields(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "customFields")
}

func (c *Client) CreateCustomField(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "customFields", body)
}

func (c *Client) GetCustomField(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "customFields", id)
}

func (c *Client) UpdateCustomField(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "customFields", id, body, updateMask...)
}

func (c *Client) DeleteCustomField(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "customFields", id)
}

// CalculatedFieldDefinition writes/reads (modern v1alpha). targetField must be an
// Active Free-Text custom field. LIVE MUTATIONS for the writes.
func (c *Client) ListCalculatedFieldDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "calculatedFieldDefinitions")
}

func (c *Client) CreateCalculatedFieldDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "calculatedFieldDefinitions", body)
}

func (c *Client) GetCalculatedFieldDefinition(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "calculatedFieldDefinitions", id)
}

func (c *Client) UpdateCalculatedFieldDefinition(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "calculatedFieldDefinitions", id, body, updateMask...)
}

func (c *Client) DeleteCalculatedFieldDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "calculatedFieldDefinitions", id)
}

// PropertySchemaDefinition writes/reads (modern v1alpha). All-scalar body
// (rawFieldName/displayName/groupName required). LIVE MUTATIONS for the writes.
func (c *Client) ListPropertySchemaDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "propertySchemaDefinitions")
}

func (c *Client) CreatePropertySchemaDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "propertySchemaDefinitions", body)
}

func (c *Client) GetPropertySchemaDefinition(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "propertySchemaDefinitions", id)
}

func (c *Client) UpdatePropertySchemaDefinition(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "propertySchemaDefinitions", id, body, updateMask...)
}

func (c *Client) DeletePropertySchemaDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "propertySchemaDefinitions", id)
}
