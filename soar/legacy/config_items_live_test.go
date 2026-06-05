package legacy

import (
	"testing"
)

// TestLiveConfigItemsReads covers the config-items ("config-items" tag) read
// surface. Every method in this tag — ConfigItemList, ConfigItemUpdate,
// ConfigItemBatchUpdate — is keyed by a specific tenantID (and the writes also
// need a freeform config-items body), so there is no zero-argument, no-setup
// read that is guaranteed green on a fresh tenant. With no list endpoint to
// derive a tenantID from, there is nothing safe to probe, so this test skips.
func TestLiveConfigItemsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	_, _ = lc, ctx
	t.Skip("no read-only endpoints in this tag")
}
