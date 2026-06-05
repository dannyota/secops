// LEGACY tier: the Siemplify external API (/api/external/v1) ontology *mapping
// rules* surface — how raw event fields map to SOAR entities. Config-as-code.
// Reads return RawJSON; writes take a freeform body.
package legacy

import "context"

// ListMappingRules returns the ontology mapping rules. body is the freeform
// selector payload (source/product/event filters).
func (c *Client) ListMappingRules(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/GetMappingRules", body)
}

// AddOrUpdateMappingRules creates or updates ontology mapping rules. body is the
// freeform rules payload. LIVE MUTATION.
func (c *Client) AddOrUpdateMappingRules(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/AddOrUpdateMappingRules", body)
}

// DeleteMappingRule deletes one mapping rule. body carries its key. LIVE MUTATION.
func (c *Client) DeleteMappingRule(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/DeleteMappingRule", body)
}

// ExportOntology exports the ontology (mapping rules) bundle for storing as code.
// body selects what to export.
func (c *Client) ExportOntology(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/ExportOntology", body)
}

// ImportOntology imports a previously exported ontology bundle. LIVE MUTATION.
func (c *Client) ImportOntology(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/ImportOntology", body)
}

// OntologyOrVisualFamilyExists reports whether an ontology record or visual
// family already exists (a pre-write existence check). body carries the key.
func (c *Client) OntologyOrVisualFamilyExists(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/ontology/IsOntologyOrVisualFamilyExists", body)
}
