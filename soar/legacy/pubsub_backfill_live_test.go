package legacy

import (
	"testing"
)

// TestLivePubSubBackfillReads covers the PubSubBackfill tag. Its only method is
// PubSubBackfillTrigger(ctx, tenantID, body) — a LIVE MUTATION that re-publishes
// historical data into the ingestion pipeline. It requires a specific tenant ID
// and a freeform request body and exposes no list/read endpoint to derive them
// from, so there is nothing safe to probe read-only here.
func TestLivePubSubBackfillReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
