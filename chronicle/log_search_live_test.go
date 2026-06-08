package chronicle_test

import (
	"errors"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveFetchRawLogLines validates the raw-log retrieval path end to end: a UDM
// search to collect each event's raw-log id, then legacyFindRawLogs to download the
// FULL bytes. It asserts the decode works and that a returned line can exceed the
// 80-char search snippet (proving the full-bytes path). Read-only; never logs
// content. Gated on SECOPS_SIEM_SMOKE=1.
func TestLiveFetchRawLogLines(t *testing.T) {
	c, ctx := liveChronicle(t)
	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)

	// Any ingesting log type works; GCP_CLOUDAUDIT is reliably present.
	lines, err := c.FetchRawLogLines(ctx, `metadata.log_type = "GCP_CLOUDAUDIT"`, start, end, 10)
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) {
			t.Skipf("FetchRawLogLines API error (input/transport, not a decode bug): HTTP %d", apiErr.Status)
		}
		t.Fatalf("FetchRawLogLines decode/logic failure: %v", err)
	}
	t.Logf("FetchRawLogLines: %d full raw line(s)", len(lines))
	if len(lines) == 0 {
		t.Skip("no raw logs in the window; nothing to assert")
	}
	maxLen := 0
	for _, l := range lines {
		if len(l.Text) > maxLen {
			maxLen = len(l.Text)
		}
		if l.Text == "" {
			t.Error("a raw line decoded to empty text")
		}
	}
	// A real raw log routinely exceeds the 80-char search snippet; if the longest is
	// <=80 we likely returned the preview, not the full bytes (the bug this guards).
	if maxLen <= 80 {
		t.Errorf("longest raw line is %d chars (<=80) — looks like the truncated snippet, not full bytes", maxLen)
	}
}
