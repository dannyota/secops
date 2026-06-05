package legacy

import (
	"testing"
)

// TestLiveCloudLoggingReads would exercise the Cloud Logging read endpoints, but
// this tag has no zero-argument reads that succeed on a fresh tenant:
//
//   - CloudLoggingGetPythonLogs(ctx, body) is a POST whose freeform legacy
//     filter/paging body cannot be safely constructed here.
//   - CloudLoggingDownloadAgentLogs(ctx, agentIdentifier, hoursBack) needs a
//     specific remote-agent identifier, and this tag exposes no list endpoint to
//     derive one from.
//
// So there is nothing green to probe; we still build a live client to keep the
// gating/skip behavior consistent, then skip.
func TestLiveCloudLoggingReads(t *testing.T) {
	liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
