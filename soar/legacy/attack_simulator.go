// LEGACY tier: the Siemplify external API (/api/external/v1) Case Management
// surface. This file covers the attacks-simulator endpoints, which create,
// export/import, and simulate custom ("simulated") cases and use cases that
// appear as Test cases in the case queue.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via the shared external* helpers.
package legacy

import (
	"context"
	"net/url"
)

// AttackSimCreateSimulatedCustomCase creates a custom (simulated) case. body is
// the freeform custom-case definition. LIVE MUTATION.
func (c *Client) AttackSimCreateSimulatedCustomCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/CreateSimulatedCustomCase", body)
}

// AttackSimGetCustomCaseDetails returns the details of a custom (simulated) case
// in your environment. body selects the case (freeform legacy payload).
func (c *Client) AttackSimGetCustomCaseDetails(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/GetCustomCaseDetails", body)
}

// AttackSimExportCustomCase exports a custom (simulated) case as a JSON file,
// selected by its name.
func (c *Client) AttackSimExportCustomCase(ctx context.Context, customCaseName string) (RawJSON, error) {
	return c.externalGet(ctx, "/attackssimulator/ExportCustomCase/"+url.PathEscape(customCaseName))
}

// AttackSimImportCustomCase imports a custom (simulated) case from a JSON file.
// body is the freeform exported-case payload. LIVE MUTATION.
func (c *Client) AttackSimImportCustomCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/ImportCustomCase", body)
}

// AttackSimDeleteUseCase deletes a given use case. body selects the use case
// (freeform legacy payload). LIVE MUTATION; this cannot be undone.
func (c *Client) AttackSimDeleteUseCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/DeleteUseCase", body)
}

// AttackSimGenerateUseCases simulates a case so it is presented in the case
// queue. body is the freeform simulation payload. LIVE MUTATION.
func (c *Client) AttackSimGenerateUseCases(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/GenerateUseCases", body)
}

// AttackSimSimulateAlert simulates a specific alert within a case; the alert is
// then presented as a Test case in your case queue. body is the freeform alert
// payload. LIVE MUTATION.
func (c *Client) AttackSimSimulateAlert(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/attackssimulator/SimulateAlert", body)
}

// AttackSimGetCustomCases returns all custom (simulated) cases in your
// environment.
func (c *Client) AttackSimGetCustomCases(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/attackssimulator/GetCustomCases")
}

// AttackSimIsCustomCaseExists reports whether a custom-case name already exists.
func (c *Client) AttackSimIsCustomCaseExists(ctx context.Context, alertName string) (RawJSON, error) {
	return c.externalGet(ctx, "/attackssimulator/IsCustomCaseExists/"+url.PathEscape(alertName))
}
