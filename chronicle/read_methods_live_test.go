package chronicle_test

import (
	"errors"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveReadMethodShapes locks the response-shape fixes for read methods the
// live API returns differently than first modeled:
//   - ValidateQuery: validity comes from errorType/errorText, not an isValid flag;
//   - ListIoCs: an association's regionCode is an object {countryOrRegion}, not a string;
//   - SearchRawLogs: a server-streamed JSON array of chunks with matches[] whose
//     logType is a nested {displayName} object.
//
// Read-only. Gated on SECOPS_SIEM_SMOKE=1.
func TestLiveReadMethodShapes(t *testing.T) {
	c, ctx := liveChronicle(t)

	// ValidateQuery: a valid query is valid; a broken one is flagged with a message.
	if v, err := c.ValidateQuery(ctx, `metadata.event_type = "USER_LOGIN"`); err != nil {
		t.Errorf("ValidateQuery(valid): %v", err)
	} else if !v.IsValid {
		t.Errorf("ValidateQuery: valid query reported invalid (queryType=%q msg=%q)", v.QueryType, v.ValidationMessage)
	}
	if bad, err := c.ValidateQuery(ctx, `this is not a valid udm query (((`); err != nil {
		t.Errorf("ValidateQuery(invalid): %v", err)
	} else if bad.IsValid || bad.ValidationMessage == "" {
		t.Errorf("ValidateQuery: broken query not flagged (isValid=%v msg=%q)", bad.IsValid, bad.ValidationMessage)
	}

	end := time.Now().UTC()
	start := end.Add(-72 * time.Hour)

	// ListIoCs must decode (the regionCode object) without a type error.
	if iocs, err := c.ListIoCs(ctx, start, end, false, 50); err != nil {
		t.Errorf("ListIoCs: %v", err)
	} else {
		t.Logf("ListIoCs: %d matches", len(iocs))
	}

	// SearchRawLogs must DECODE the streamed array. A bad-query 400 or a server 500
	// is an *APIError* (input/transport, tolerated here); only a decode failure —
	// the shape bug — should fail this test.
	logs, err := c.SearchRawLogs(ctx, `raw = "login"`, nil, start, end, 10)
	var apiErr *chronicle.APIError
	switch {
	case err == nil:
		t.Logf("SearchRawLogs: %d entries", len(logs))
	case errors.As(err, &apiErr):
		t.Logf("SearchRawLogs: API error (input/transport, not a decode bug): HTTP %d", apiErr.Status)
	default:
		t.Errorf("SearchRawLogs decode failure (the shape bug): %v", err)
	}
}
