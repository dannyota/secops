package legacy

import (
	"testing"
)

// TestLiveEntitySearchReads covers the entity-search tag (EntitySearch* methods:
// EntitySearchCount, EntitySearchListEntities, EntitySearchEverything). Every
// method in this tag is a POST-with-body search that requires a freeform
// search-request payload (filters/pagination) which cannot be safely constructed
// to succeed on a tenant with no prior setup, and the tag has no zero-argument
// list/get reads to derive one from. So there are no read-only endpoints to probe
// green here; we still establish the live client so the file compiles and runs.
func TestLiveEntitySearchReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag")
}
