package legacy

import (
	"testing"
)

// TestLiveDashboardsReads exercises the read-only Dashboards endpoints (safe).
// Runs under SECOPS_SOAR_SMOKE=1.
//
// Only the zero-argument, no-prior-setup read is included: the widget-definition
// catalog (GetDashboardWidgetDefinitions) is a built-in, tenant-independent list,
// so it is green on a fresh tenant. The remaining Dashboard reads
// (DashboardGetWidgetValues / DashboardGetWidgetCaseIds) are POST searches that
// require a widget-query body we cannot safely synthesize, so they are excluded.
func TestLiveDashboardsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "dashboards/GetDashboardWidgetDefinitions", func() (RawJSON, error) {
		return lc.DashboardWidgetDefinitionList(ctx)
	})
}

// No CRUD test: the Dashboards tag exposes AddOrUpdate/Delete mutations but no
// list endpoint to enumerate dashboards, capture a created id, or find a record
// by name. runLifecycle requires list+create+update+delete, so a correct,
// reviewable lifecycle cannot be built for this tag — reads only.
