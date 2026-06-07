package chronicle

import (
	"context"
	"testing"
	"time"
)

// TestSearchUDMPageMoreAvailable verifies the server's moreDataAvailable flag is
// surfaced (so a caller can warn that results were truncated at --limit) and that
// the thin SearchUDM wrapper still returns just the events.
func TestSearchUDMPageMoreAvailable(t *testing.T) {
	c := alertTestClient(t, `{"events":[{"name":"e1"}],"moreDataAvailable":true}`)
	start := time.Unix(0, 0).UTC()
	end := start.Add(time.Hour)
	query := `metadata.event_type = "x"`

	events, more, err := c.SearchUDMPage(context.Background(), query, start, end, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
	if !more {
		t.Error("moreAvailable = false, want true (server reported moreDataAvailable)")
	}

	// The wrapper drops the flag but still returns the events.
	ev, err := c.SearchUDM(context.Background(), query, start, end, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 {
		t.Errorf("SearchUDM got %d events, want 1", len(ev))
	}
}
