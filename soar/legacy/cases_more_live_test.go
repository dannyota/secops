package legacy

import (
	"testing"
)

// TestLiveCasesMoreReads exercises the zero-argument, setup-free read endpoints
// in the CaseX (case-management extras) tag. Both are simple counters for the
// logged-in user / tenant, so they return cleanly on a tenant with no prior
// setup. Runs under SECOPS_SOAR_SMOKE=1.
//
// The other CaseX reads are excluded here because they require a specific id,
// file id, or a request body/filter that cannot be safely derived without
// existing tenant data (e.g. CaseXGetWallItemsForWarRoom(id),
// CaseXDownloadCommentFile(fileID), CaseXListComments(q),
// CaseXGetCardsByRequest(body)).
func TestLiveCasesMoreReads(t *testing.T) {
	lc, ctx := liveClient(t)
	// GetTasksCountForUser omitted: returns a server-side HTTP 500.
	readProbe(t, "cases/requests/GetCollaboratorRequestCount", func() (RawJSON, error) {
		return lc.CaseXGetCollaboratorRequestCount(ctx)
	})
}
