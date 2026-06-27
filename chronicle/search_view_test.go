package chronicle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/auth"
)

func TestURLSafeEventID(t *testing.T) {
	// Standard base64 id with + / = must become URL-safe, unpadded, byte-identical.
	in := "AAAAAMg5KhH+bJy5h6bk4morDdAAAAAAAQAAAEkAAAA="
	got := urlSafeEventID(in)
	if strings.ContainsAny(got, "+/=") {
		t.Errorf("urlSafeEventID(%q) = %q — must not contain +/= (path 400s otherwise)", in, got)
	}
	want, _ := base64.StdEncoding.DecodeString(in)
	gotBytes, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil || !bytes.Equal(want, gotBytes) {
		t.Errorf("urlSafeEventID changed the bytes: err=%v", err)
	}
}

// captureBodyRT records the last request body/URL and returns a canned response, so a
// test can assert both the wire request shape and the response decode.
type captureBodyRT struct {
	resp string
	body []byte
	url  string
}

func (r *captureBodyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		r.body, _ = io.ReadAll(req.Body)
	}
	r.url = req.URL.String()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.resp)),
		Header:     make(http.Header),
	}, nil
}

func captureBodyClient(t *testing.T, resp string) (*Client, *captureBodyRT) {
	t.Helper()
	rt := &captureBodyRT{resp: resp}
	c, err := NewClient(
		Settings{ProjectID: "pid", ProjectNumber: "000000000000", Region: "us", CustomerID: "cust"},
		auth.OAuth(),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c, rt
}

func TestDecodeStreamChunks(t *testing.T) {
	// Array body.
	arr, err := decodeStreamChunks[csvExportChunk]([]byte(`[{"complete":false},{"complete":true}]`))
	if err != nil || len(arr) != 2 || !arr[1].Complete {
		t.Fatalf("array decode: got %+v err %v", arr, err)
	}
	// Single object body.
	one, err := decodeStreamChunks[csvExportChunk]([]byte(`{"complete":true}`))
	if err != nil || len(one) != 1 || !one[0].Complete {
		t.Fatalf("object decode: got %+v err %v", one, err)
	}
	// Empty body.
	none, err := decodeStreamChunks[csvExportChunk]([]byte("  "))
	if err != nil || none != nil {
		t.Fatalf("empty decode: got %+v err %v", none, err)
	}
}

func TestAssembleSearchViewReplaceSemantics(t *testing.T) {
	chunks := []udmSearchViewChunk{
		{Progress: 0.3, Events: &udmEventList{Events: []json.RawMessage{[]byte(`{"e":1}`), []byte(`{"e":2}`)}}, BaselineEventsCount: 0},
		{
			Progress: 0.7, Events: &udmEventList{Events: []json.RawMessage{[]byte(`{"e":1}`), []byte(`{"e":2}`), []byte(`{"e":3}`)}}, BaselineEventsCount: 100,
			AIOverview: &udmViewAIOverview{AISummary: "summary"},
		},
		{Progress: 1, Complete: true, BaselineEventsCount: 100, AvailableResultCount: 3}, // terminal: events omitted
	}
	v, err := assembleSearchView(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Events) != 3 {
		t.Errorf("Events = %d, want 3 (last non-empty chunk replaces, not appends)", len(v.Events))
	}
	if v.BaselineEventsCount != 100 || v.AvailableResultCount != 3 {
		t.Errorf("counts = %d/%d, want 100/3", v.BaselineEventsCount, v.AvailableResultCount)
	}
	if !v.Complete || v.AIOverview != "summary" {
		t.Errorf("complete=%v aiOverview=%q", v.Complete, v.AIOverview)
	}
}

func TestAssembleSearchViewOperationOnly(t *testing.T) {
	v, err := assembleSearchView([]udmSearchViewChunk{{Operation: "projects/p/locations/l/instances/i/operations/s-udm-x"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.OperationID == "" || len(v.Events) != 0 {
		t.Errorf("operation-only: OperationID=%q events=%d", v.OperationID, len(v.Events))
	}
}

func TestAssembleSearchViewQueryError(t *testing.T) {
	_, err := assembleSearchView([]udmSearchViewChunk{{QueryValidationErrors: []json.RawMessage{[]byte(`{"text":"bad"}`)}}})
	if err == nil {
		t.Fatal("want error on queryValidationErrors")
	}
}

func TestExportUDMSearchCSVAggregates(t *testing.T) {
	body := `[{"csv":{"row":["timestamp,user","2026,alice"]},"progress":0.5},` +
		`{"csv":{"row":["2026,bob"],"timestamps":["t1"]},"tooManyEvents":true,"complete":true,` +
		`"failureCsvFieldValidations":[{"field":"bogus"}]}]`
	c := alertTestClient(t, body)
	res, err := c.ExportUDMSearchCSVResult(context.Background(), `metadata.event_type = "x"`,
		time.Unix(0, 0).UTC(), time.Unix(3600, 0).UTC(), []string{"timestamp", "user"}, true)
	if err != nil {
		t.Fatal(err)
	}
	wantCSV := "timestamp,user\n2026,alice\n2026,bob"
	if res.CSV != wantCSV {
		t.Errorf("CSV = %q, want %q", res.CSV, wantCSV)
	}
	if !res.TooManyEvents || !res.Complete {
		t.Errorf("flags: tooMany=%v complete=%v", res.TooManyEvents, res.Complete)
	}
	if len(res.InvalidFields) != 1 || res.InvalidFields[0] != "bogus" {
		t.Errorf("InvalidFields = %v", res.InvalidFields)
	}
}

func TestExportUDMSearchCSVRequestShape(t *testing.T) {
	c, rt := captureBodyClient(t, `[{"csv":{"row":["h"]},"complete":true}]`)
	if _, err := c.ExportUDMSearchCSV(context.Background(), `x`,
		time.Unix(0, 0).UTC(), time.Unix(3600, 0).UTC(), []string{"timestamp", "user"}, true); err != nil {
		t.Fatal(err)
	}
	got := string(rt.body)
	for _, want := range []string{`"fields":{"fields":["timestamp","user"]}`, `"queryType":"UDM_QUERY"`, `"caseInsensitive":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("request body missing %s; got %s", want, got)
		}
	}
	if !strings.Contains(rt.url, "legacy:legacyFetchUdmSearchCsv") {
		t.Errorf("url = %s", rt.url)
	}
}

func TestExportUDMSearchCSVValidation(t *testing.T) {
	c := alertTestClient(t, `[]`)
	if _, err := c.ExportUDMSearchCSV(context.Background(), "  ", time.Unix(0, 0).UTC(), time.Unix(1, 0).UTC(), []string{"timestamp"}, true); err == nil {
		t.Error("want error on empty query")
	}
	if _, err := c.ExportUDMSearchCSV(context.Background(), "x", time.Unix(0, 0).UTC(), time.Unix(1, 0).UTC(), nil, true); err == nil {
		t.Error("want error on empty fields")
	}
}

func TestFindUDMEventsParse(t *testing.T) {
	// Single object.
	c := alertTestClient(t, `{"udmEventGroups":[{"events":[{"e":1}]}]}`)
	r, err := c.FindUDMEvents(context.Background(), []string{"id1"}, nil, true)
	if err != nil || len(r.UDMEvents()) != 1 {
		t.Fatalf("object: %+v err %v", r, err)
	}
	// Streamed array → groups merged.
	c2 := alertTestClient(t, `[{"udmEventGroups":[{"events":[{"e":1}]}]},{"udmEventGroups":[{"events":[{"e":2}]}]}]`)
	r2, err := c2.FindUDMEvents(context.Background(), []string{"id1", "id2"}, nil, false)
	if err != nil || len(r2.UDMEvents()) != 2 {
		t.Fatalf("array merge: %+v err %v", r2, err)
	}
	// No ids/tokens → error.
	if _, err := c.FindUDMEvents(context.Background(), nil, nil, false); err == nil {
		t.Error("want error with no ids/tokens")
	}
}

func TestStreamSearchParse(t *testing.T) {
	body := `{"operation":{"name":"projects/p/locations/l/instances/i/operations/s-udm-x","done":true,` +
		`"response":{"complete":true,"baselineEventsCount":5,"availableResultCount":2,"events":{"events":[{"e":1},{"e":2}]}}}}`
	c := alertTestClient(t, body)
	page, err := c.StreamSearch(context.Background(), "operations/s-udm-x", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.BaselineEventsCount != 5 || !page.Complete || !page.Done {
		t.Errorf("page = %+v", page)
	}
	// Error path.
	c2 := alertTestClient(t, `{"operation":{"error":{"code":3,"message":"boom"}}}`)
	if _, err := c2.StreamSearch(context.Background(), "s-udm-x", 0, 0); err == nil {
		t.Fatal("want error on operation.error")
	}
}

func TestNormalizeOperationID(t *testing.T) {
	cases := map[string]string{
		"s-udm-x":            "s-udm-x",
		"operations/s-udm-x": "s-udm-x",
		"projects/p/locations/l/instances/i/operations/s-udm-x": "s-udm-x",
		"  s-udm-x  ": "s-udm-x",
	}
	for in, want := range cases {
		if got := normalizeOperationID(in); got != want {
			t.Errorf("normalizeOperationID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranslateNLToUDMWithTimeRange(t *testing.T) {
	c := alertTestClient(t, `{"query":"metadata.event_type = \"x\"","timeRange":{"startTime":"2026-01-01T00:00:00Z","endTime":"2026-01-01T01:00:00Z"}}`)
	res, err := c.TranslateNLToUDMWithTimeRange(context.Background(), "show x last hour")
	if err != nil {
		t.Fatal(err)
	}
	if res.TimeRange == nil || res.TimeRange.StartTime.IsZero() || res.TimeRange.EndTime.IsZero() {
		t.Fatalf("timeRange not parsed: %+v", res.TimeRange)
	}
	// Soft failure: message, no query → error; back-compat wrapper returns "".
	c2 := alertTestClient(t, `{"message":"could not generate"}`)
	if q, err := c2.TranslateNLToUDM(context.Background(), "gibberish"); err == nil || q != "" {
		t.Errorf("want error+empty on message-only; got q=%q err=%v", q, err)
	}
}

func TestSavedSearchesParseAndShared(t *testing.T) {
	body := `{"searchQueries":[` +
		`{"name":"projects/p/locations/l/instances/i/users/me/searchQueries/aaa","displayName":"shared one","metadata":{"sharingMode":"MODE_SHARED_WITH_CUSTOMER"}},` +
		`{"name":"projects/p/locations/l/instances/i/users/me/searchQueries/bbb","displayName":"private one"}]}`
	c := alertTestClient(t, body)
	list, err := c.ListSavedSearches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d saved searches, want 2", len(list))
	}
	if !list[0].Shared() {
		t.Error("first should be shared")
	}
	if list[1].Shared() {
		t.Error("second (no metadata) should be private")
	}
}

func TestSavedSearchNameResolution(t *testing.T) {
	c := alertTestClient(t, `{}`)
	if got := c.savedSearchName("aaa"); !strings.HasSuffix(got, "/users/me/searchQueries/aaa") {
		t.Errorf("id form = %q", got)
	}
	full := "projects/p/locations/l/instances/i/users/me/searchQueries/bbb"
	if got := c.savedSearchName(full); got != full {
		t.Errorf("full-name form = %q, want unchanged", got)
	}
}

func TestCreateSavedSearchDefaultsAndValidation(t *testing.T) {
	// Empty query → client-side error.
	c := alertTestClient(t, `{}`)
	if _, err := c.CreateSavedSearch(context.Background(), SavedSearch{Query: "  "}); err == nil {
		t.Error("want error on empty query")
	}
	// Defaults applied + searchQueryId param set.
	c2, rt := captureBodyClient(t, `{"name":"projects/p/locations/l/instances/i/users/me/searchQueries/x"}`)
	if _, err := c2.CreateSavedSearch(context.Background(), SavedSearch{DisplayName: "d", Query: `x`}); err != nil {
		t.Fatal(err)
	}
	got := string(rt.body)
	for _, want := range []string{`"queryType":"QUERY_TYPE_UDM_QUERY"`, `"queryLanguage":"QUERY_LANGUAGE_YL2"`} {
		if !strings.Contains(got, want) {
			t.Errorf("create body missing default %s; got %s", want, got)
		}
	}
	if !strings.Contains(rt.url, "searchQueryId=") {
		t.Errorf("create url missing searchQueryId param: %s", rt.url)
	}
}

func TestUpdateSavedSearchRequiresMask(t *testing.T) {
	c := alertTestClient(t, `{}`)
	if _, err := c.UpdateSavedSearch(context.Background(), "aaa", SavedSearch{DisplayName: "x"}); err == nil {
		t.Error("want error when no updateMask fields given")
	}
}
