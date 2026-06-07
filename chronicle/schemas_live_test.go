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
	if len(logSchemas) == 0 {
		return
	}

	// Exercise the single-resource reads on a real log type — the list endpoints
	// don't cover them, and these are the live-vs-documented-shape risk.
	lt := logSchemas[0].LogType
	if lt == "" {
		lt = logSchemas[0].ID
	}
	if lt == "" {
		return
	}
	// The setting is a singleton that returns defaults (an empty/minimal body) for a
	// log type with no custom setting — like riskConfig. Validate the call answers
	// and decodes (a non-empty Raw), not that a custom setting exists.
	setting, err := c.GetLogTypeSetting(ctx, lt)
	switch {
	case err != nil:
		t.Errorf("GetLogTypeSetting(%q): %v", lt, err)
	case len(setting.Raw) == 0:
		t.Errorf("GetLogTypeSetting(%q) returned no body", lt)
	default:
		t.Logf("GetLogTypeSetting(%q): %d bytes", lt, len(setting.Raw))
	}
}
