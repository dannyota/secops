package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

// hintRT returns failStatus + a google.rpc.RetryInfo hint on the first failFor
// calls, then 200 — so a test can assert whether the retry loop honored the delay.
type hintRT struct {
	calls      int
	failFor    int
	failStatus int
	delay      string // RetryInfo retryDelay, e.g. "0.05s"
}

func (r *hintRT) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	if r.calls <= r.failFor {
		body := `{"error":{"details":[` +
			`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"` + r.delay + `"}]}}`
		return &http.Response{StatusCode: r.failStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

// TestRetryHonorsHint locks the quota fix: a 429 carrying a RetryInfo delay is
// retried after waiting (at least) that delay, and the call then recovers — even
// with the local backoff zeroed, proving the wait came from the server's hint.
func TestRetryHonorsHint(t *testing.T) {
	old := baseBackoff
	baseBackoff = 0 // no local backoff — any wait must come from the honored hint
	t.Cleanup(func() { baseBackoff = old })

	rt := &hintRT{failFor: 1, failStatus: http.StatusTooManyRequests, delay: "0.05s"}
	c, err := NewClient(
		Settings{ProjectID: "p", ProjectNumber: "0", Region: "r", CustomerID: "c"},
		auth.OAuth(), WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := c.get(context.Background(), "x", nil); err != nil {
		t.Fatalf("should recover after honoring the hint, got %v", err)
	}
	if rt.calls != 2 {
		t.Errorf("want 2 calls (one retry), got %d", rt.calls)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("expected to wait ~the 50ms hint before retrying, only %v elapsed", elapsed)
	}
}

// TestRetry5xxIgnoresHint locks the fix that a Retry-After/RetryInfo hint is
// honored ONLY for a 429 (quota); a transient 5xx must NOT block for a (possibly
// proxy-set) long hint — it uses the short backoff and retries promptly.
func TestRetry5xxIgnoresHint(t *testing.T) {
	old := baseBackoff
	baseBackoff = 0
	t.Cleanup(func() { baseBackoff = old })

	rt := &hintRT{failFor: 1, failStatus: http.StatusServiceUnavailable, delay: "10s"}
	c, err := NewClient(
		Settings{ProjectID: "p", ProjectNumber: "0", Region: "r", CustomerID: "c"},
		auth.OAuth(), WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := c.get(context.Background(), "x", nil); err != nil {
		t.Fatalf("503 GET should retry and recover, got %v", err)
	}
	if rt.calls != 2 {
		t.Errorf("want 2 calls (one retry), got %d", rt.calls)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a 5xx must NOT honor the 10s hint; waited %v", elapsed)
	}
}
