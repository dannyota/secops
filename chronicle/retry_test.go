package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// countingRT returns a fixed status for every call and counts requests, so a test
// can assert whether the client retried.
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

// TestRetryPolicy locks the fix: a mutating POST/PATCH must NOT be retried on 5xx
// (the request may have already taken effect — retrying duplicates it), while
// idempotent GET/PUT/DELETE are retried, and 429 is retried for any method.
func TestRetryPolicy(t *testing.T) {
	old := baseBackoff
	baseBackoff = 0
	t.Cleanup(func() { baseBackoff = old })

	mk := func(status int) (*Client, *countingRT) {
		rt := &countingRT{status: status}
		c, err := NewClient(
			Settings{ProjectID: "p", ProjectNumber: "0", Region: "r", CustomerID: "c"},
			auth.OAuth(), WithHTTPClient(&http.Client{Transport: rt}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return c, rt
	}
	ctx := context.Background()
	const wantRetries = maxRetries + 1 // one initial + maxRetries

	cases := []struct {
		name      string
		status    int
		call      func(*Client) error
		wantCalls int
	}{
		{"POST 500 not retried", 500, func(c *Client) error { return c.post(ctx, "x", map[string]any{}, nil) }, 1},
		{"PATCH 500 not retried", 500, func(c *Client) error { return c.patch(ctx, "x", map[string]any{}, nil) }, 1},
		{"GET 500 retried", 500, func(c *Client) error { return c.get(ctx, "x", nil) }, wantRetries},
		{"DELETE 503 retried", 503, func(c *Client) error { return c.do(ctx, http.MethodDelete, "x", nil, nil) }, wantRetries},
		{"POST 429 retried", 429, func(c *Client) error { return c.post(ctx, "x", map[string]any{}, nil) }, wantRetries},
	}
	for _, tc := range cases {
		c, rt := mk(tc.status)
		_ = tc.call(c)
		if rt.calls != tc.wantCalls {
			t.Errorf("%s: got %d calls, want %d", tc.name, rt.calls, tc.wantCalls)
		}
	}
}
