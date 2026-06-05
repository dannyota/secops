package legacy

import "testing"

// TestLiveMarketplaceReads exercises the marketplace/store catalog read endpoints
// (safe; read-only). These return the integration/power-up/report/use-case
// catalogs and store UI presets, which exist on any tenant with no prior setup.
// Runs under SECOPS_SOAR_SMOKE=1.
//
// Excluded on purpose: GetStoreIntegrationDependencies / TestStoreIntegration
// need a specific integration identifier; GetStoreIntegrationFullDetails and
// ExportStoreUseCase are POST-with-body selectors. The mutating endpoints
// (SaveStoreIntegrationConfigurationProperties, ReinstallAllIntegrations) are
// not cosmetic config CRUD, so this tag has no lifecycle test.
func TestLiveMarketplaceReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "store/GetIntegrationsStoreData", func() (RawJSON, error) {
		return lc.GetIntegrationsStoreData(ctx, false)
	})
	readProbe(t, "store/GetPowerUpsStoreData", func() (RawJSON, error) {
		return lc.GetPowerUpsStoreData(ctx)
	})
	readProbe(t, "store/GetReportsStoreData", func() (RawJSON, error) {
		return lc.GetReportsStoreData(ctx)
	})
	readProbe(t, "store/GetUsecasesCards", func() (RawJSON, error) {
		return lc.GetUsecasesCards(ctx)
	})
	readProbe(t, "store/GetHtmlViewPresets", func() (RawJSON, error) {
		return lc.GetHtmlViewPresets(ctx)
	})
}
