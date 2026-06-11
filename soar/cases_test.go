package soar_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar"
)

// TestCaseUnmarshalTolerant locks the schema-tolerant decode of Title/Assignee:
// the v1alpha case schema has used different keys across revisions, so Case picks
// the first present one and always preserves the full body in Raw.
func TestCaseUnmarshalTolerant(t *testing.T) {
	cases := []struct {
		name           string
		body           string
		wantTitle      string
		wantAssignee   string
		wantStatus     string
		wantPriorityID string
	}{
		{"displayName+assignee", `{"displayId":"42","displayName":"Phishing wave","assignee":"alice","status":"OPENED","priority":"PRIORITY_HIGH"}`, "Phishing wave", "alice", "OPENED", "42"},
		{"title+assignedUser", `{"displayId":"7","title":"Beacon","assignedUser":"bob","status":"CLOSED"}`, "Beacon", "bob", "CLOSED", "7"},
		{"owner-fallback", `{"displayId":"9","displayName":"X","owner":"carol"}`, "X", "carol", "", "9"},
		{"none", `{"displayId":"1"}`, "", "", "", "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c soar.Case
			if err := json.Unmarshal([]byte(tc.body), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if c.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", c.Title, tc.wantTitle)
			}
			if c.Assignee != tc.wantAssignee {
				t.Errorf("Assignee = %q, want %q", c.Assignee, tc.wantAssignee)
			}
			if c.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", c.Status, tc.wantStatus)
			}
			if c.DisplayID != tc.wantPriorityID {
				t.Errorf("DisplayID = %q, want %q", c.DisplayID, tc.wantPriorityID)
			}
			if len(c.Raw) == 0 {
				t.Error("Raw not preserved")
			}
		})
	}
}

// TestListCasesTyped locks that the typed list decodes the page into Case values
// (with the tolerant Title/Assignee) rather than raw JSON.
func TestListCasesTyped(t *testing.T) {
	rt := &fixedBodyRT{body: `{"cases":[{"displayId":"5","displayName":"Recon","assignee":"dave","status":"OPENED"}],"nextPageToken":""}`}
	c := newCaptureClient(t, rt)
	got, err := c.ListCasesTyped(context.Background(), soar.CaseListOptions{PageSize: 10})
	if err != nil {
		t.Fatalf("ListCasesTyped: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d cases, want 1", len(got))
	}
	if got[0].Title != "Recon" || got[0].Assignee != "dave" || got[0].DisplayID != "5" {
		t.Errorf("decoded case = %+v", got[0])
	}
}

// fixedBodyRT returns a fixed body for any request (offline, no auth).
type fixedBodyRT struct{ body string }

func (r *fixedBodyRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

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

// statusBodyRT answers each call with the next queued status+body (offline).
type statusBodyRT struct {
	queue []struct {
		status int
		body   string
	}
	queries []url.Values
}

func (r *statusBodyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.queries = append(r.queries, req.URL.Query())
	next := r.queue[0]
	if len(r.queue) > 1 {
		r.queue = r.queue[1:]
	}
	return &http.Response{
		StatusCode: next.status,
		Body:       io.NopCloser(strings.NewReader(next.body)),
		Header:     make(http.Header),
	}, nil
}

// TestCountCases locks the totalSize count semantics: the full filtered count
// comes back on a pageSize=1 request, and a zero-match 204 (empty body)
// counts as 0 instead of failing to decode.
func TestCountCases(t *testing.T) {
	rt := &statusBodyRT{queue: []struct {
		status int
		body   string
	}{{200, `{"cases":[{"id":1}],"totalSize":17,"nextPageToken":"tok"}`}}}
	c := newCaptureClient(t, rt)
	n, err := c.CountCases(context.Background(), "status = 'OPENED'")
	if err != nil || n != 17 {
		t.Errorf("CountCases = %d, %v; want 17", n, err)
	}
	q := rt.queries[0]
	if q.Get("pageSize") != "1" || q.Get("filter") != "status = 'OPENED'" {
		t.Errorf("count query = %v", q)
	}

	rt = &statusBodyRT{queue: []struct {
		status int
		body   string
	}{{204, ""}}}
	c = newCaptureClient(t, rt)
	if n, err := c.CountCases(context.Background(), "assignee = 'nobody'"); err != nil || n != 0 {
		t.Errorf("zero-match CountCases = %d, %v; want 0", n, err)
	}
}

// TestCountCasesByPriority locks the per-priority composition: one count per
// token, the base filter parenthesized in front.
func TestCountCasesByPriority(t *testing.T) {
	rt := &statusBodyRT{queue: []struct {
		status int
		body   string
	}{{200, `{"totalSize":2}`}}}
	c := newCaptureClient(t, rt)
	counts, err := c.CountCasesByPriority(context.Background(), "status = 'OPENED'")
	if err != nil {
		t.Fatalf("CountCasesByPriority: %v", err)
	}
	if len(counts) != len(soar.CasePriorityTokens) {
		t.Errorf("counts = %v", counts)
	}
	if len(rt.queries) != len(soar.CasePriorityTokens) {
		t.Fatalf("%d requests, want %d", len(rt.queries), len(soar.CasePriorityTokens))
	}
	want := "(status = 'OPENED') and (priority = 'PRIORITY_INFO')"
	if got := rt.queries[0].Get("filter"); got != want {
		t.Errorf("composed filter = %q, want %q", got, want)
	}
}
