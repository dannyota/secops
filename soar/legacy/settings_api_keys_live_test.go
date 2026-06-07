package legacy

import "testing"

// TestLiveListAPIKeys validates the API-key metadata read on the live external API
// (GET /settings/GetApiKeys). Read-only; gated on SECOPS_SOAR_SMOKE=1. The
// no-secret invariant (the typed APIKey carries no key field) is covered offline
// in TestAPIKeyDecodeDropsSecret.
func TestLiveListAPIKeys(t *testing.T) {
	c, ctx := liveClient(t)
	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	t.Logf("OK ListAPIKeys: %d key(s)", len(keys))
	for _, k := range keys {
		if k.Name == "" {
			t.Errorf("api key %d has no name (metadata decode issue)", k.ID)
		}
	}
}
