package chronicle

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/auth"
)

// statsCaptureRT records the last request (URL + body) and returns a canned response,
// so RunStatsQuery's request shape and response parsing can be asserted offline.
type statsCaptureRT struct {
	url, body string
	status    int
	resp      string
}

func (r *statsCaptureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.body = string(b)
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(r.resp)),
		Header:     make(http.Header),
	}, nil
}

func statsClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	c, err := NewClient(
		Settings{ProjectID: "pid", ProjectNumber: "000000000000", Region: "us", CustomerID: "cust"},
		auth.OAuth(),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRunStatsQueryRequest locks the request shape: a POST to dashboardQueries:execute
// carrying the query and an input.timeWindow with absolute start/end times.
func TestRunStatsQueryRequest(t *testing.T) {
	rt := &statsCaptureRT{resp: `{"results":[]}`}
	c := statsClient(t, rt)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	q := "metadata.log_type != \"\"\nmatch: $lt = metadata.log_type\noutcome: $c = count(metadata.id)"
	if _, err := c.RunStatsQuery(context.Background(), q, start, end, false); err != nil {
		t.Fatalf("RunStatsQuery: %v", err)
	}
	if !strings.Contains(rt.url, "/dashboardQueries:execute") {
		t.Errorf("expected dashboardQueries:execute, got %s", rt.url)
	}
	var body struct {
		Query struct {
			Query string `json:"query"`
			Input struct {
				TimeWindow struct {
					StartTime string `json:"startTime"`
					EndTime   string `json:"endTime"`
				} `json:"timeWindow"`
			} `json:"input"`
		} `json:"query"`
	}
	if err := json.Unmarshal([]byte(rt.body), &body); err != nil {
		t.Fatalf("decode request body: %v (%s)", err, rt.body)
	}
	if body.Query.Query != q {
		t.Errorf("query not round-tripped: %q", body.Query.Query)
	}
	if body.Query.Input.TimeWindow.StartTime != "2026-01-02T03:04:05.000000Z" {
		t.Errorf("startTime = %q (want microsecond precision)", body.Query.Input.TimeWindow.StartTime)
	}
	if body.Query.Input.TimeWindow.EndTime != "2026-01-03T03:04:05.000000Z" {
		t.Errorf("endTime = %q (want microsecond precision)", body.Query.Input.TimeWindow.EndTime)
	}
}

// TestRunStatsQueryTranspose locks parsing of the execute response's top-level
// `results` (column-major ColumnData) into row-major columns/rows.
func TestRunStatsQueryTranspose(t *testing.T) {
	rt := &statsCaptureRT{resp: `{"results":[
		{"column":"lt","values":[{"value":{"stringVal":"WINEVTLOG"}},{"value":{"stringVal":"GCP_DNS"}}]},
		{"column":"c","values":[{"value":{"int64Val":"42"}},{"value":{"int64Val":"7"}}]}
	]}`}
	c := statsClient(t, rt)
	res, err := c.RunStatsQuery(context.Background(), "q\nmatch: $lt = x\noutcome: $c = count(x)",
		time.Unix(0, 0), time.Unix(3600, 0), false)
	if err != nil {
		t.Fatalf("RunStatsQuery: %v", err)
	}
	if want := []string{"lt", "c"}; len(res.Columns) != 2 || res.Columns[0] != want[0] || res.Columns[1] != want[1] {
		t.Fatalf("columns = %v, want %v", res.Columns, want)
	}
	if res.TotalRows != 2 {
		t.Fatalf("rows = %d, want 2", res.TotalRows)
	}
	if got := string(res.Rows[0]["lt"]); got != `"WINEVTLOG"` {
		t.Errorf("row0 lt = %s", got)
	}
	if got := string(res.Rows[0]["c"]); got != `42` {
		t.Errorf("row0 c = %s (want bare number)", got)
	}
}

// TestRunStatsQuerySevereError surfaces a SEVERE queryRuntimeError (a 200 carrying
// an in-body error) as a clean error rather than an empty result.
func TestRunStatsQuerySevereError(t *testing.T) {
	rt := &statsCaptureRT{resp: `{"results":[],"queryRuntimeErrors":[
		{"errorTitle":"Invalid query","errorDescription":"unknown field foo","errorSeverity":"SEVERE"}
	]}`}
	c := statsClient(t, rt)
	_, err := c.RunStatsQuery(context.Background(), "q\nmatch: $x = foo", time.Unix(0, 0), time.Unix(3600, 0), false)
	if err == nil {
		t.Fatal("expected an error for a SEVERE runtime error")
	}
	if !strings.Contains(err.Error(), "unknown field foo") {
		t.Errorf("error should carry the description: %v", err)
	}
}

// TestRunStatsQueryWarningNotFatal keeps a WARNING (e.g. row-limit) non-fatal — the
// rows still come back.
func TestRunStatsQueryWarningNotFatal(t *testing.T) {
	rt := &statsCaptureRT{resp: `{"results":[
		{"column":"c","values":[{"value":{"int64Val":"1"}}]}
	],"queryRuntimeErrors":[
		{"errorTitle":"Row limit","errorDescription":"default row limit exceeded","errorSeverity":"WARNING"}
	]}`}
	c := statsClient(t, rt)
	res, err := c.RunStatsQuery(context.Background(), "q\noutcome: $c = count(x)", time.Unix(0, 0), time.Unix(3600, 0), false)
	if err != nil {
		t.Fatalf("a WARNING must not be fatal: %v", err)
	}
	if res.TotalRows != 1 {
		t.Errorf("rows = %d, want 1", res.TotalRows)
	}
	// The warning text must be surfaced, not silently dropped, so a truncated result
	// is never shown as complete.
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "row limit exceeded") {
		t.Errorf("warnings = %v, want the row-limit notice surfaced", res.Warnings)
	}
}

// TestRunStatsQueryUnknownSeverityFatal locks the default-fatal policy: a runtime
// error whose severity is neither WARNING nor unspecified is treated as fatal, so a
// real failure is never reported as a clean empty result.
func TestRunStatsQueryUnknownSeverityFatal(t *testing.T) {
	rt := &statsCaptureRT{resp: `{"results":[],"queryRuntimeErrors":[
		{"errorTitle":"Boom","errorDescription":"backend exploded","errorSeverity":"CRITICAL"}
	]}`}
	c := statsClient(t, rt)
	_, err := c.RunStatsQuery(context.Background(), "q\noutcome: $c = count(x)", time.Unix(0, 0), time.Unix(3600, 0), false)
	if err == nil || !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("an unknown severity must be fatal, got err=%v", err)
	}
}
