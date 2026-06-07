package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// recordingRT records whether any request was sent (and returns 200), so a test
// can assert a guard short-circuited before reaching the wire.
type recordingRT struct{ called bool }

func (r *recordingRT) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
}

func newRecordingClient(t *testing.T) (*Client, *recordingRT) {
	t.Helper()
	rt := &recordingRT{}
	c, err := NewClient(Settings{ProjectID: "p", ProjectNumber: "0", Region: "r", CustomerID: "c"},
		auth.OAuth(), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	return c, rt
}

// TestUpdateFeedRefusesEmptyMask: an UpdateFeed with no fields must not fire a
// maskless PATCH (which the server could read as a full-resource replace, blanking
// the feed). It returns an error before any request.
func TestUpdateFeedRefusesEmptyMask(t *testing.T) {
	c, rt := newRecordingClient(t)
	if _, err := c.UpdateFeed(context.Background(), "fe_1", "", "", "", "", nil); err == nil {
		t.Error("UpdateFeed with no fields should error")
	}
	if rt.called {
		t.Error("UpdateFeed sent a request despite an empty update mask")
	}
}

// TestEditChartRefusesEmptyMask: an editChart whose objects carry only name/etag
// (no editable field) yields an empty editMask; it must refuse rather than POST
// editMask="" (which updates nothing yet consumes the etag).
func TestEditChartRefusesEmptyMask(t *testing.T) {
	c, rt := newRecordingClient(t)
	in := EditChartInput{DashboardChart: []byte(`{"name":"ch_1","etag":"e"}`)}
	if _, err := c.EditChart(context.Background(), "db_1", in); err == nil {
		t.Error("EditChart with no editable fields should error")
	}
	if rt.called {
		t.Error("EditChart sent a request despite an empty edit mask")
	}
}

// TestFeedID verifies the trailing-segment extraction tolerates a bare id, a full
// resource name, and a stray trailing slash.
func TestFeedID(t *testing.T) {
	cases := map[string]string{
		"fe_1": "fe_1",
		"projects/p/locations/r/instances/c/feeds/fe_1":  "fe_1",
		"projects/p/locations/r/instances/c/feeds/fe_1/": "fe_1",
	}
	for in, want := range cases {
		if got := feedID(in); got != want {
			t.Errorf("feedID(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFeedLogType verifies the short→full log-type expansion the feeds write path
// requires (the API rejects a bare id as a "malformed resource name").
func TestFeedLogType(t *testing.T) {
	c, err := NewClient(Settings{ProjectID: "p", Region: "r", CustomerID: "c"}, auth.OAuth())
	if err != nil {
		t.Fatal(err)
	}
	full := "projects/p/locations/r/instances/c/logTypes/NGINX"
	if got := c.feedLogType("NGINX"); got != full {
		t.Errorf("feedLogType(short) = %q, want %q", got, full)
	}
	if got := c.feedLogType(full); got != full {
		t.Errorf("feedLogType(full) = %q, want it left unchanged", got)
	}
	if got := c.feedLogType(""); got != "" {
		t.Errorf("feedLogType(empty) = %q, want empty", got)
	}
}
