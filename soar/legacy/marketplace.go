// LEGACY tier: Siemplify external API (/api/external/v1) MARKETPLACE (store)
// surface — the integration/power-up/use-case store and integration install/test.
// Reads return RawJSON; writes take a freeform body.
package legacy

import (
	"context"
	"net/url"
)

// GetIntegrationsStoreData returns the integrations available in the store.
// staging selects the staging catalog.
func (c *Client) GetIntegrationsStoreData(ctx context.Context, staging bool) (RawJSON, error) {
	q := url.Values{}
	if staging {
		q.Set("isStaging", "true")
	}
	return c.externalGetQuery(ctx, "/store/GetIntegrationsStoreData", q)
}

// GetPowerUpsStoreData returns the power-ups available in the store.
func (c *Client) GetPowerUpsStoreData(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/store/GetPowerUpsStoreData")
}

// GetReportsStoreData returns the reports available in the store.
func (c *Client) GetReportsStoreData(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/store/GetReportsStoreData")
}

// GetUsecasesCards returns the use-case cards in the store.
func (c *Client) GetUsecasesCards(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/store/GetUsecasesCards")
}

// GetHtmlViewPresets returns the store HTML view presets.
func (c *Client) GetHtmlViewPresets(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/store/GetHtmlViewPresets")
}

// GetStoreIntegrationDependencies returns a store integration's dependencies.
func (c *Client) GetStoreIntegrationDependencies(ctx context.Context, integrationIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/store/GetIntegrationDependencies/"+url.PathEscape(integrationIdentifier))
}

// GetStoreIntegrationFullDetails returns full details for a store integration.
// body selects the integration.
func (c *Client) GetStoreIntegrationFullDetails(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/store/GetIntegrationFullDetails", body)
}

// TestStoreIntegration tests a store integration instance.
func (c *Client) TestStoreIntegration(ctx context.Context, integrationInstanceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/store/TestIntegration/"+url.PathEscape(integrationInstanceID))
}

// ExportStoreUseCase exports a use-case bundle. body selects what to export.
func (c *Client) ExportStoreUseCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/store/ExportUseCase", body)
}

// SaveStoreIntegrationConfigurationProperties saves integration configuration
// properties from the store. LIVE MUTATION.
func (c *Client) SaveStoreIntegrationConfigurationProperties(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/store/SaveIntegrationConfigurationProperties", body)
}

// ReinstallAllIntegrations reinstalls every integration from the store. LIVE
// MUTATION with very high blast radius — use with extreme care.
func (c *Client) ReinstallAllIntegrations(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/store/ReinstallAllIntegrations", body)
}

// --- Integration install (Content Hub) ---
//
// GetPackageDetails and DownloadAndInstallIntegration are the per-integration
// Content-Hub install flow. They live under /api/external/v1/store/ (reachable on
// this AppKey external surface) but are NOT in the swagger snapshot
// (third_party/siemplify-swagger.json) — their shapes come from the live Content Hub
// UI requests, per the "diff against the real UI request" house rule. The documented
// modern twin is the v1alpha marketplaceIntegrations :install / :uninstall API;
// UNINSTALL is v1alpha-only (the external store API installs but has no uninstall, and
// there is no per-connector-definition delete on the external API — a custom "Copy of"
// connector definition is removed by uninstalling its parent custom integration).

// GetPackageDetails returns a store package's details prior to install. body is the
// freeform selector the UI sends (the package identifier/version). Read-only.
func (c *Client) GetPackageDetails(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/store/GetPackageDetails", body)
}

// InstallIntegrationRequest installs one integration from the local store; fields
// match the Content-Hub UI request. Identifier is the integration id (e.g.
// "CloudIdentity"); OverrideMapping replaces existing ontology mapping on (re)install.
type InstallIntegrationRequest struct {
	Identifier      string `json:"identifier"`
	Name            string `json:"name"`
	IsCertified     bool   `json:"isCertified"`
	Version         int    `json:"version"`
	OverrideMapping bool   `json:"overrideMapping"`
}

// DownloadAndInstallIntegration installs one integration package from the store. LIVE
// MUTATION — adds an integration plus its connector/job/action DEFINITIONS to the
// tenant. Not reversible via this surface (uninstall is v1alpha-only), so there is no
// self-cleaning smoke for it.
func (c *Client) DownloadAndInstallIntegration(ctx context.Context, req InstallIntegrationRequest) (RawJSON, error) {
	return c.externalPost(ctx, "/store/DownloadAndInstallIntegrationFromLocalStore", req)
}
