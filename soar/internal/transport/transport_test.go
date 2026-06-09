package transport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// countingRT returns a fixed status for every call and counts how many requests
// it saw, so a test can assert whether the transport retried.
type countingRT struct {
	status int
	calls  int
}

func (r *countingRT) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func newTestTransport(rt http.RoundTripper) *Transport {
	return New(Settings{BaseURL: "https://t.example.com", ProjectNumber: "0", Region: "us", CustomerID: "c"},
		auth.SOARAppKey("k"), &http.Client{Transport: rt})
}

// TestRetryPolicy locks the fix: a mutating POST must NOT be retried on 5xx (the
// request may have already taken effect server-side — retrying duplicates it),
// while idempotent GETs are retried, and 429 is retried for any method.
func TestRetryPolicy(t *testing.T) {
	old := baseBackoff
	baseBackoff = 0
	t.Cleanup(func() { baseBackoff = old })
	ctx := context.Background()
	cases := []struct {
		name      string
		method    int // 0 = External POST, 1 = External GET
		status    int
		wantCalls int
	}{
		{"post 500 not retried", 0, 500, 1},
		{"post 503 not retried", 0, 503, 1},
		{"post 429 retried", 0, 429, maxRetries + 1},
		{"get 500 retried", 1, 500, maxRetries + 1},
		{"get 404 not retried", 1, 404, 1},
		{"post 400 not retried", 0, 400, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &countingRT{status: tc.status}
			tr := newTestTransport(rt)
			var out map[string]any
			if tc.method == 0 {
				_ = tr.External(ctx, http.MethodPost, "/x", map[string]any{"a": 1}, &out)
			} else {
				_ = tr.External(ctx, http.MethodGet, "/x", nil, &out)
			}
			if rt.calls != tc.wantCalls {
				t.Errorf("%s: made %d calls, want %d", tc.name, rt.calls, tc.wantCalls)
			}
		})
	}
}

func TestIdempotentMethod(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete} {
		if !idempotentMethod(m) {
			t.Errorf("%s should be idempotent", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPatch} {
		if idempotentMethod(m) {
			t.Errorf("%s should NOT be idempotent", m)
		}
	}
}

func TestErrorStringRedactsURL(t *testing.T) {
	err := (&Error{
		Method:    http.MethodPost,
		URL:       "https://tenant.example.com/api/external/v1/private/path",
		Status:    http.StatusInternalServerError,
		Body:      "boom",
		RequestID: "rid-9",
	}).Error()
	for _, want := range []string{"POST request failed", "HTTP 500", "request-id: rid-9", "boom"} {
		if !strings.Contains(err, want) {
			t.Fatalf("error string missing %q: %s", want, err)
		}
	}
	if strings.Contains(err, "tenant.example.com") || strings.Contains(err, "/api/external/v1/private/path") {
		t.Fatalf("error string leaked URL: %s", err)
	}
}

// TestPaginateV1AlphaTruncation: when the page cap is hit while a token remains,
// the listing is truncated, so PaginateV1Alpha returns an error rather than
// silently presenting a partial result as complete. A run that drains the token
// before the cap returns nil.
func TestPaginateV1AlphaTruncation(t *testing.T) {
	// Always returns a non-empty token → never drains within the cap.
	calls := 0
	err := PaginateV1Alpha(3, func(string) (string, error) {
		calls++
		return "more", nil
	})
	if err == nil {
		t.Error("expected a truncation error when the page cap is exhausted with a token remaining")
	}
	if calls != 3 {
		t.Errorf("fetched %d pages, want exactly the 3-page cap", calls)
	}

	// Drains on the 2nd page → completes cleanly.
	calls = 0
	err = PaginateV1Alpha(5, func(string) (string, error) {
		calls++
		if calls == 2 {
			return "", nil
		}
		return "more", nil
	})
	if err != nil {
		t.Errorf("unexpected error on a complete listing: %v", err)
	}
	if calls != 2 {
		t.Errorf("fetched %d pages, want 2 (stopped when the token drained)", calls)
	}
}
