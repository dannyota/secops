package legacy

import (
	"testing"
)

// TestLiveCasesChatReads covers the casechat (per-case messaging) read surface.
//
// Every CaseChat* read requires a caller-supplied identifier — a caseID
// (CaseChatList, CaseChatNewMessagesCount) or an attachmentID
// (CaseChatGetAttachment, CaseChatGetAttachmentPreview) — and the tag exposes no
// zero-argument list from which to derive one. On a tenant with no prior setup
// there is no case or attachment to address, so there is nothing that is
// guaranteed-green to probe. We still construct the live client so the file
// compiles and participates in the smoke gate, then skip.
func TestLiveCasesChatReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
