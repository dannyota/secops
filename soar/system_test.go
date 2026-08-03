package soar_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar"
)

type countStatusRT struct {
	calls  int
	status int
}

func (r *countStatusRT) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestWithoutRetriesLimitsSystemProbeToOneAttempt(t *testing.T) {
	rt := &countStatusRT{status: http.StatusServiceUnavailable}
	c, err := soar.NewClient(
		soar.Settings{BaseURL: "https://tenant.example", ProjectNumber: "1", Region: "us", CustomerID: "c"},
		auth.SOARAppKey("key"),
		soar.WithHTTPClient(&http.Client{Transport: rt}),
		soar.WithoutRetries(),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := c.SystemGetVersion(context.Background()); err == nil {
		t.Fatal("SystemGetVersion succeeded, want HTTP 503 error")
	}
	if rt.calls != 1 {
		t.Fatalf("SystemGetVersion made %d attempts, want exactly 1", rt.calls)
	}
}
