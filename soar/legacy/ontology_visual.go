// LEGACY tier: the Siemplify external API (/api/external/v1) ontology *visual
// family* surface — the entity-grouping/visualization families and their rules.
// Reads return RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/url"
)

// ListVisualFamilies returns every visual family.
func (c *Client) ListVisualFamilies(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/ontology/GetVisualFamilies")
}

// GetVisualFamily returns one visual family. body carries its selector.
func (c *Client) GetVisualFamily(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/GetFamily", body)
}

// GetRelatedEntitiesByFamilyName returns the entities related to a visual family.
func (c *Client) GetRelatedEntitiesByFamilyName(ctx context.Context, familyName string) (RawJSON, error) {
	return c.externalGet(ctx, "/ontology/GetRelatedEntitiesByFamilyName/"+url.PathEscape(familyName))
}

// IsVisualFamilyExists reports whether a visual family already exists. body
// carries the name.
func (c *Client) IsVisualFamilyExists(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/IsVisualFamilyExists", body)
}

// AddOrUpdateVisualFamily creates or updates a visual family. LIVE MUTATION.
func (c *Client) AddOrUpdateVisualFamily(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/AddOrUpdateVisualFamily", body)
}

// AddOrUpdateVisualFamilyRules creates or updates a visual family's rules.
// LIVE MUTATION.
func (c *Client) AddOrUpdateVisualFamilyRules(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/AddOrUpdateVisualFamilyRules", body)
}

// DeleteVisualFamilyRule deletes one visual family rule. body carries its key.
// LIVE MUTATION.
func (c *Client) DeleteVisualFamilyRule(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/DeleteVisualFamilyRule", body)
}

// DeleteFamilyData deletes a visual family's data by id. DEVIATION: the legacy
// route is a GET that mutates — it deletes despite the verb. LIVE MUTATION;
// this cannot be undone.
func (c *Client) DeleteFamilyData(ctx context.Context, familyID string) (RawJSON, error) {
	return c.externalGet(ctx, "/ontology/DeleteFamilyData/"+url.PathEscape(familyID))
}

// DuplicateVisualFamilyForSettings clones a visual family. LIVE MUTATION.
func (c *Client) DuplicateVisualFamilyForSettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/DuplicateVisualFamilyForSettings", body)
}

// ExportVisualFamily exports a visual family bundle for storing as code.
func (c *Client) ExportVisualFamily(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/ExportVisualFamily", body)
}

// ImportVisualFamily imports a previously exported visual family. LIVE MUTATION.
func (c *Client) ImportVisualFamily(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/ImportVisualFamily", body)
}

// UpdateVisualFamilyImage updates a visual family's image. LIVE MUTATION.
func (c *Client) UpdateVisualFamilyImage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/UpdateVisualFamilyImage", body)
}
