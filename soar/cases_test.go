package soar_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar"
)

// captureRT records the last request's query and returns an empty cases page, so
// a test can assert which query parameters ListCasesOpts sent — offline, no auth.
type captureRT struct{ lastQuery url.Values }

func (r *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastQuery = req.URL.Query()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"cases":[],"nextPageToken":""}`)),
		Header:     make(http.Header),
	}, nil
}

func newCaptureClient(t *testing.T, rt http.RoundTripper) *soar.Client {
	t.Helper()
	c, err := soar.NewClient(
		soar.Settings{BaseURL: "https://t.example.com", ProjectNumber: "0", Region: "us", CustomerID: "c"},
		auth.SOARAppKey("k"), soar.WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestListCasesOptsQuery locks that ListCasesOpts forwards filter / orderBy /
// expand / pageSize as v1alpha query parameters (matching the web UI request).
func TestListCasesOptsQuery(t *testing.T) {
	rt := &captureRT{}
	c := newCaptureClient(t, rt)
	if _, err := c.ListCasesOpts(context.Background(), soar.CaseListOptions{
		PageSize: 50, Filter: "status = 'OPENED'", OrderBy: "updateTime desc",
		Expand: "products,tasks,tags,closureDetails,sla,alertsSla",
	}); err != nil {
		t.Fatalf("ListCasesOpts: %v", err)
	}
	for k, want := range map[string]string{
		"pageSize": "50",
		"filter":   "status = 'OPENED'",
		"orderBy":  "updateTime desc",
		"expand":   "products,tasks,tags,closureDetails,sla,alertsSla",
	} {
		if got := rt.lastQuery.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
}

// TestListCasesNoOptsQuery locks that the bare ListCases wrapper sends only
// pageSize — no empty filter/orderBy/expand keys.
func TestListCasesNoOptsQuery(t *testing.T) {
	rt := &captureRT{}
	c := newCaptureClient(t, rt)
	if _, err := c.ListCases(context.Background(), 25); err != nil {
		t.Fatalf("ListCases: %v", err)
	}
	if got := rt.lastQuery.Get("pageSize"); got != "25" {
		t.Errorf("pageSize = %q, want 25", got)
	}
	for _, k := range []string{"filter", "orderBy", "expand"} {
		if _, present := rt.lastQuery[k]; present {
			t.Errorf("unexpected query param %q for bare ListCases", k)
		}
	}
}
