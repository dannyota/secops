package chronicle_test

import (
	"testing"
)

// TestLiveRBACRead validates the data-access read path (labels + scopes list).
// The endpoints resolve cleanly even when the tenant has none configured. Gated
// on SECOPS_SIEM_SMOKE=1; read-only.
func TestLiveRBACRead(t *testing.T) {
	c, ctx := liveChronicle(t)
	labels, err := c.ListDataAccessLabels(ctx)
	if err != nil {
		t.Fatalf("ListDataAccessLabels: %v", err)
	}
	t.Logf("dataAccessLabels: %d", len(labels))
	scopes, err := c.ListDataAccessScopes(ctx)
	if err != nil {
		t.Fatalf("ListDataAccessScopes: %v", err)
	}
	t.Logf("dataAccessScopes: %d", len(scopes))
}

// TestLiveRiskConfigRead validates the corrected risk-config path
// (GET {instance}/riskConfig — returns system defaults if no custom config).
// Read-only. Gated on SECOPS_SIEM_SMOKE=1.
func TestLiveRiskConfigRead(t *testing.T) {
	c, ctx := liveChronicle(t)
	rc, err := c.GetRiskConfig(ctx)
	if err != nil {
		t.Fatalf("GetRiskConfig: %v", err)
	}
	t.Logf("riskConfig name=%q detectionScore=%v alertScore=%v", rc.Name,
		rc.DefaultDetectionRiskScore, rc.DefaultAlertRiskScore)
}
