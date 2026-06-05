package legacy

import "testing"

// TestLiveResourcesReads: the only zero-argument Resources read
// (DownloadAuditControllerActionsCsv) returns CSV, not JSON, so it is not a JSON
// read probe; the by-id getters (action results, full case details, entity
// insights) need a live case/result/insight id. No green JSON read-only probe.
func TestLiveResourcesReads(t *testing.T) {
	t.Skip("Resources has no JSON read-only endpoint callable without prior tenant state")
}
