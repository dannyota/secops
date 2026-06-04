package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// captureRT records the request URL and returns a canned 200 so no network or
// credentials are needed.
type captureRT struct{ url string }

func (r *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

// TestLegacyEndpointPaths locks the critical review fix: the legacy RPCs must hit
// the /legacy:<method> segment with the correct project form.
func TestLegacyEndpointPaths(t *testing.T) {
	rt := &captureRT{}
	c, err := NewClient(
		Settings{ProjectID: "pid", ProjectNumber: "000000000000", Region: "us", CustomerID: "cust"},
		auth.OAuth(),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := c.FindRawLogs(ctx, map[string]any{}); err != nil {
		t.Fatalf("FindRawLogs: %v", err)
	}
	if !strings.Contains(rt.url, "/instances/cust/legacy:legacyFindRawLogs") {
		t.Errorf("FindRawLogs URL missing /legacy: segment: %s", rt.url)
	}
	if !strings.Contains(rt.url, "/projects/000000000000/") {
		t.Errorf("FindRawLogs must use the project NUMBER form: %s", rt.url)
	}

	if _, err := c.BatchGetCases(ctx, []string{"uuid-1", "uuid-2"}); err != nil {
		t.Fatalf("BatchGetCases: %v", err)
	}
	if !strings.Contains(rt.url, "/instances/cust/legacy:legacyBatchGetCases") {
		t.Errorf("BatchGetCases URL missing /legacy: segment: %s", rt.url)
	}
	if !strings.Contains(rt.url, "/projects/pid/") {
		t.Errorf("BatchGetCases must use the project ID form: %s", rt.url)
	}
	if !strings.Contains(rt.url, "names=uuid-1") || !strings.Contains(rt.url, "names=uuid-2") {
		t.Errorf("BatchGetCases must send repeated names params: %s", rt.url)
	}
}
