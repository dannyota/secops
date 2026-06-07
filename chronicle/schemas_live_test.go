package chronicle_test

import (
	"testing"
)

// TestLiveSchemaDiscoveryRead validates the ingestion schema read path: list the
// feed source type schemas, then list the log type schemas for the first one.
// Read-only. Gated on SECOPS_SIEM_SMOKE=1.
func TestLiveSchemaDiscoveryRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	schemas, err := c.ListFeedSourceTypeSchemas(ctx)
	if err != nil {
		t.Fatalf("ListFeedSourceTypeSchemas: %v", err)
	}
	t.Logf("feedSourceTypeSchemas: %d", len(schemas))
	if len(schemas) == 0 {
		t.Skip("instance exposes no feed source type schemas to drill into")
	}

	first := schemas[0]
	sourceType := first.ID
	if sourceType == "" {
		sourceType = first.FeedSourceType
	}
	t.Logf("first: id=%q feedSourceType=%q displayName=%q", first.ID, first.FeedSourceType, first.DisplayName)

	logSchemas, err := c.ListLogTypeSchemas(ctx, sourceType)
	if err != nil {
		t.Fatalf("ListLogTypeSchemas(%q): %v", sourceType, err)
	}
	t.Logf("logTypeSchemas[%s]: %d", sourceType, len(logSchemas))
}
