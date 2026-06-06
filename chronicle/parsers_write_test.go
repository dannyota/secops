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

// cannedRT captures the outgoing request (URL + body) and returns a fixed body.
type cannedRT struct {
	url, body string
	resp      string
}

func (r *cannedRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.body = string(b)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.resp)),
		Header:     make(http.Header),
	}, nil
}

func newCannedClient(t *testing.T, rt http.RoundTripper) *Client {
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

// TestRunParserDecode pins the response shape: parsedEvents is an OBJECT
// {"events":[{"event":{...}}]} (not a bare array), and the input log is echoed
// back. It also asserts the request hits :runParser with base64-encoded cbn+log.
func TestRunParserDecode(t *testing.T) {
	const resp = `{"runParserResults":[{
		"log":"ZHVtbXk=",
		"parsedEvents":{"events":[
			{"event":{"metadata":{"eventType":"GENERIC_EVENT"}}},
			{"event":{"metadata":{"eventType":"NETWORK_CONNECTION"}}}
		]}
	}]}`
	rt := &cannedRT{resp: resp}
	c := newCannedClient(t, rt)

	got, err := c.RunParser(context.Background(), "NGINX", "filter{}", []string{"dummy"})
	if err != nil {
		t.Fatalf("RunParser: %v", err)
	}
	if len(got.RunParserResults) != 1 {
		t.Fatalf("got %d results, want 1", len(got.RunParserResults))
	}
	res := got.RunParserResults[0]
	if res.ParsedEvents == nil || len(res.ParsedEvents.Events) != 2 {
		t.Fatalf("parsedEvents not decoded as {events:[...]}: %+v", res.ParsedEvents)
	}
	if !strings.Contains(string(res.ParsedEvents.Events[0]), "GENERIC_EVENT") {
		t.Errorf("first event payload not preserved: %s", res.ParsedEvents.Events[0])
	}

	// Request: numeric-project path, :runParser, base64 cbn + log.
	if !strings.Contains(rt.url, "/projects/000000000000/") {
		t.Errorf("runParser must use the project NUMBER form: %s", rt.url)
	}
	if !strings.Contains(rt.url, "logTypes/NGINX:runParser") {
		t.Errorf("runParser URL wrong: %s", rt.url)
	}
	var sent struct {
		Parser struct {
			CBN string `json:"cbn"`
		} `json:"parser"`
		Log []string `json:"log"`
	}
	if err := json.Unmarshal([]byte(rt.body), &sent); err != nil {
		t.Fatalf("request body not JSON: %v\n%s", err, rt.body)
	}
	if dec, _ := base64.StdEncoding.DecodeString(sent.Parser.CBN); string(dec) != "filter{}" {
		t.Errorf("cbn not base64-encoded source: %q", sent.Parser.CBN)
	}
	if len(sent.Log) != 1 {
		t.Fatalf("want 1 log, got %d", len(sent.Log))
	}
	if dec, _ := base64.StdEncoding.DecodeString(sent.Log[0]); string(dec) != "dummy" {
		t.Errorf("log not base64-encoded: %q", sent.Log[0])
	}
}

// TestCreateReferenceListNormalizesName locks the reconcile-identity fix: Create
// echoes the project NUMBER in the returned name, but the SDK rewrites it to the
// project-ID form so it matches what List returns (stable identity).
func TestCreateReferenceListNormalizesName(t *testing.T) {
	// Server echoes the project NUMBER form.
	rt := &cannedRT{resp: `{"name":"projects/000000000000/locations/us/instances/cust/referenceLists/my_list","displayName":"my_list"}`}
	c := newCannedClient(t, rt)

	rl, err := c.CreateReferenceList(context.Background(), "my_list", "d", []string{"a"}, "")
	if err != nil {
		t.Fatalf("CreateReferenceList: %v", err)
	}
	want := "projects/pid/locations/us/instances/cust/referenceLists/my_list" // id form
	if rl.Name != want {
		t.Errorf("create name not normalized to id form:\n got %q\nwant %q", rl.Name, want)
	}
}

// TestCreateParserEncodesCBN verifies CreateParser base64-encodes the source and
// hits the numeric-project parsers collection.
func TestCreateParserEncodesCBN(t *testing.T) {
	rt := &cannedRT{resp: `{"name":"projects/000000000000/locations/us/instances/cust/logTypes/NGINX/parsers/pa_1","state":"INACTIVE"}`}
	c := newCannedClient(t, rt)

	p, err := c.CreateParser(context.Background(), "NGINX", "filter{}", false)
	if err != nil {
		t.Fatalf("CreateParser: %v", err)
	}
	if p.State != "INACTIVE" {
		t.Errorf("state = %q, want INACTIVE", p.State)
	}
	if !strings.Contains(rt.url, "/projects/000000000000/") || !strings.Contains(rt.url, "logTypes/NGINX/parsers") {
		t.Errorf("create URL wrong: %s", rt.url)
	}
	var sent struct {
		CBN                  string `json:"cbn"`
		ValidatedOnEmptyLogs bool   `json:"validated_on_empty_logs"`
	}
	if err := json.Unmarshal([]byte(rt.body), &sent); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if dec, _ := base64.StdEncoding.DecodeString(sent.CBN); string(dec) != "filter{}" {
		t.Errorf("cbn not base64-encoded: %q", sent.CBN)
	}
}
