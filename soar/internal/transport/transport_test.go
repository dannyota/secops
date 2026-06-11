package transport

import (
	"context"
	"io"
	"net/http"
	"net/url"
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

// recordingRT captures the last request (method, URL, headers, body) and
// answers 200 with a fixed body.
type recordingRT struct {
	lastMethod string
	lastURL    string
	lastCT     string
	lastOver   string
	lastBody   string
	respBody   string
}

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastMethod = req.Method
	r.lastURL = req.URL.String()
	r.lastCT = req.Header.Get("Content-Type")
	r.lastOver = req.Header.Get("X-HTTP-Method-Override")
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.lastBody = string(b)
	}
	body := r.respBody
	if body == "" {
		body = `{}`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

// TestMethodOverrideForLongGET locks the long-filter fallback: a GET whose URL
// exceeds the cap is sent as POST with X-HTTP-Method-Override: GET and the
// query as a form body (format stays on the URL); a short GET stays a GET.
func TestMethodOverrideForLongGET(t *testing.T) {
	rt := &recordingRT{}
	tr := newTestTransport(rt)

	q := url.Values{"filter": {"status = 'OPENED'"}}
	if err := tr.V1Alpha(context.Background(), http.MethodGet, "cases", nil, nil, Query(q)); err != nil {
		t.Fatalf("short GET: %v", err)
	}
	if rt.lastMethod != http.MethodGet || rt.lastOver != "" {
		t.Errorf("short GET sent as %s override=%q", rt.lastMethod, rt.lastOver)
	}

	long := url.Values{"filter": {strings.Repeat("x", 3000)}, "pageSize": {"1"}}
	if err := tr.V1Alpha(context.Background(), http.MethodGet, "cases", nil, nil, Query(long)); err != nil {
		t.Fatalf("long GET: %v", err)
	}
	if rt.lastMethod != http.MethodPost {
		t.Errorf("long GET method = %s, want POST", rt.lastMethod)
	}
	if rt.lastOver != "GET" {
		t.Errorf("override header = %q, want GET", rt.lastOver)
	}
	if rt.lastCT != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", rt.lastCT)
	}
	if !strings.Contains(rt.lastURL, "format=camel") || strings.Contains(rt.lastURL, "filter=") {
		t.Errorf("URL must keep only format: %s", rt.lastURL[:min(120, len(rt.lastURL))])
	}
	form, err := url.ParseQuery(rt.lastBody)
	if err != nil || form.Get("pageSize") != "1" || len(form.Get("filter")) != 3000 {
		t.Errorf("form body lost params: err=%v pageSize=%q filterLen=%d", err, form.Get("pageSize"), len(form.Get("filter")))
	}
	if form.Get("format") != "" {
		t.Error("format must stay on the URL, not the form body")
	}
}
