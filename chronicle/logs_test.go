package chronicle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// logsRT returns a fixed body and records the URL.
type logsRT struct {
	body string
	url  string
}

func (r *logsRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(r.body)), Header: make(http.Header)}, nil
}

func logsClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	c, err := NewClient(
		Settings{ProjectID: "pid", ProjectNumber: "000000000000", Region: "us", CustomerID: "cust"},
		auth.OAuth(), WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestFetchSampleLogLines locks the logTypes/<type>/logs list + base64 decode, and
// that the request uses the project NUMBER form under /logTypes/<type>/logs.
func TestFetchSampleLogLines(t *testing.T) {
	line := `192.0.2.9 - - "GET /healthz" 200`
	rt := &logsRT{body: `{"logs":[
		{"name":"x","data":"` + base64.StdEncoding.EncodeToString([]byte(line)) + `","logEntryTime":"2024-10-10T00:00:00Z"},
		{"name":"y","data":"bm90LWJhc2U2NA=="}
	]}`}
	c := logsClient(t, rt)
	lines, err := c.FetchSampleLogLines(context.Background(), "KONG_GATEWAY", 25, "")
	if err != nil {
		t.Fatalf("FetchSampleLogLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Text != line {
		t.Errorf("line0 = %q, want %q", lines[0].Text, line)
	}
	if !strings.Contains(rt.url, "/projects/000000000000/") || !strings.Contains(rt.url, "/logTypes/KONG_GATEWAY/logs") {
		t.Errorf("URL = %s", rt.url)
	}
	if !strings.Contains(rt.url, "pageSize=25") {
		t.Errorf("URL missing pageSize: %s", rt.url)
	}
}

// TestParsingErrorMessage locks the tolerant message extraction: an object with a
// message key, a bare string, and a fallback.
func TestParsingErrorMessage(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{`{"code":2,"message":"LOG_PARSING_CBN_ERROR: boom"}`, "LOG_PARSING_CBN_ERROR: boom"},
		{`"plain string error"`, "plain string error"},
		{`{"unknownKey":"x"}`, `{"unknownKey":"x"}`},
	}
	for _, tc := range cases {
		pe := ParsingError{Error: json.RawMessage(tc.raw)}
		if got := pe.Message(); got != tc.want {
			t.Errorf("Message(%s) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
