package legacy

import "testing"

// TestLiveApprovalLinksReads covers the approval-links tag. That surface exposes
// exactly one external endpoint — ApprovalLinkApply, a LIVE MUTATION that resumes
// a waiting playbook step from a freeform link payload we cannot safely construct
// on a fresh tenant. There are no zero-argument read endpoints, so this test only
// establishes the live client and skips.
func TestLiveApprovalLinksReads(t *testing.T) {
	lc, ctx := liveClient(t)
	_, _ = lc, ctx
	t.Skip("no read-only endpoints in this tag")
}
