package chronicle

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// rawBodyRT returns a fixed body for any request and records the last URL — enough to
// exercise the raw-log decode offline (no network, no credentials).
type rawBodyRT struct {
	body string
	url  string
}

func (r *rawBodyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

func rawLinesClient(t *testing.T, rt http.RoundTripper) *Client {
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

// TestFindRawLogsByIDsURL locks the GET shape: the /legacy:legacyFindRawLogs verb,
// the project NUMBER form, and one ?ids= param per id.
func TestFindRawLogsByIDsURL(t *testing.T) {
	rt := &rawBodyRT{body: `{"rawLogs":[]}`}
	c := rawLinesClient(t, rt)
	if _, err := c.FindRawLogsByIDs(context.Background(), []string{"id-a", "id-b", ""}); err != nil {
		t.Fatalf("FindRawLogsByIDs: %v", err)
	}
	if !strings.Contains(rt.url, "/legacy:legacyFindRawLogs") {
		t.Errorf("URL missing /legacy: segment: %s", rt.url)
	}
	if !strings.Contains(rt.url, "/projects/000000000000/") {
		t.Errorf("FindRawLogsByIDs must use the project NUMBER form: %s", rt.url)
	}
	if !strings.Contains(rt.url, "ids=id-a") || !strings.Contains(rt.url, "ids=id-b") {
		t.Errorf("URL missing ids params: %s", rt.url)
	}
	if strings.Contains(rt.url, "ids=&") || strings.HasSuffix(rt.url, "ids=") {
		t.Errorf("empty id should be skipped, not sent: %s", rt.url)
	}
}

// TestFindRawLogLinesDecode locks the decode: base64 logBytes -> text, the nested
// rawLogs[].rawLogs[] structure flattened, provenance lifted, and a non-base64
// value passed through verbatim.
func TestFindRawLogLinesDecode(t *testing.T) {
	line1 := `192.0.2.1 - - [10/Oct/2024:13:55:36] "GET /health HTTP/1.1" 200`
	line2 := `{"level":"info","msg":"request","path":"/api/v1"}`
	b1 := base64.StdEncoding.EncodeToString([]byte(line1))
	b2 := base64.StdEncoding.EncodeToString([]byte(line2))
	// Two id-groups; second group also has a value that is NOT valid base64.
	body := `{"rawLogs":[
		{"rawLogs":[
			{"logBytes":"` + b1 + `","sourceProduct":"kong","timestamp":"2024-10-10T13:55:36Z","type":"KONG_GATEWAY"}
		]},
		{"rawLogs":[
			{"logBytes":"` + b2 + `","type":"KONG_GATEWAY"},
			{"logBytes":"not_base64 with spaces","type":"KONG_GATEWAY"}
		]}
	]}`
	rt := &rawBodyRT{body: body}
	c := rawLinesClient(t, rt)

	lines, err := c.FindRawLogLines(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("FindRawLogLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (flattened across groups)", len(lines))
	}
	if lines[0].Text != line1 {
		t.Errorf("line0 = %q, want %q", lines[0].Text, line1)
	}
	if lines[0].SourceProduct != "kong" || lines[0].LogType != "KONG_GATEWAY" || lines[0].Timestamp == "" {
		t.Errorf("line0 provenance not lifted: %+v", lines[0])
	}
	if lines[1].Text != line2 {
		t.Errorf("line1 = %q, want %q", lines[1].Text, line2)
	}
	// Non-base64 logBytes passes through verbatim (no corruption).
	if lines[2].Text != "not_base64 with spaces" {
		t.Errorf("line2 (non-base64) = %q, want verbatim passthrough", lines[2].Text)
	}
}
