package legacy

import "testing"

// TestLiveRetentionReads covers the Retention tag. Every Retention method is a
// destructive POST-with-body mutation (bulk case deletion, system-data purge,
// user-data purge) that permanently destroys data and requires a request body we
// cannot safely construct. The tag exposes no zero-argument read endpoints, so
// there is nothing safe to probe under a read-only smoke run.
func TestLiveRetentionReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
