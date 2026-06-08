package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

type tiRelatedRT struct {
	body string
	req  *http.Request
}

func (r *tiRelatedRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

func tiRelatedClient(t *testing.T, body string) (*Client, *tiRelatedRT) {
	t.Helper()
	rt := &tiRelatedRT{body: body}
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

func TestFetchRelatedThreatCollections(t *testing.T) {
	c, rt := tiRelatedClient(t, `{"threatCollections":[{
		"name":"projects/000000000000/locations/us/instances/cust/threatCollections/report--1",
		"threatCollectionType":"REPORT",
		"displayName":"Example report",
		"altNames":["REP.00.001"]
	}]}`)

	got, err := c.FetchRelatedThreatCollections(context.Background(), RelatedThreatCollectionQuery{
		Type:     RelatedThreatCollectionReport,
		Ioc:      "ioc_1",
		OrderBy:  "name+",
		PageSize: 10,
		MaxPages: 1,
	})
	if err != nil {
		t.Fatalf("FetchRelatedThreatCollections: %v", err)
	}
	if len(got) != 1 || got[0].ID != "report--1" || got[0].AltNames[0] != "REP.00.001" {
		t.Fatalf("decoded related collection wrong: %+v", got)
	}
	if !strings.Contains(rt.req.URL.Path, "/v1/projects/000000000000/locations/us/instances/cust/threatCollections:fetchRelated") {
		t.Fatalf("path = %s", rt.req.URL.Path)
	}
	q := rt.req.URL.Query()
	if q.Get("threatCollectionType") != "REPORT" {
		t.Errorf("threatCollectionType = %q", q.Get("threatCollectionType"))
	}
	wantIoc := "projects/pid/locations/us/instances/cust/iocs/ioc_1"
	if q.Get("ioc") != wantIoc {
		t.Errorf("ioc = %q, want %q", q.Get("ioc"), wantIoc)
	}
	if q.Get("orderBy") != "name+" || q.Get("pageSize") != "10" {
		t.Errorf("query = %s", rt.req.URL.RawQuery)
	}
}

func TestFetchIocMatchMetadata(t *testing.T) {
	c, rt := tiRelatedClient(t, `{"iocMatchMetadata":[
		{"threatCollection":"CAMP.00.001","iocMatchesCount":7}
	]}`)

	got, err := c.FetchIocMatchMetadata(context.Background(), "CAMP.00.001")
	if err != nil {
		t.Fatalf("FetchIocMatchMetadata: %v", err)
	}
	if len(got) != 1 || got[0].ThreatCollection != "CAMP.00.001" || got[0].IocMatchesCount != 7 {
		t.Fatalf("decoded metadata wrong: %+v", got)
	}
	if !strings.Contains(rt.req.URL.Path, "/v1/projects/000000000000/locations/us/instances/cust/threatCollections:fetchIocMatchMetadata") {
		t.Fatalf("path = %s", rt.req.URL.Path)
	}
	if got := rt.req.URL.Query()["threatCollections"]; len(got) != 1 || got[0] != "CAMP.00.001" {
		t.Fatalf("threatCollections query = %#v", got)
	}
}

func TestFetchRelatedIoCs(t *testing.T) {
	c, rt := tiRelatedClient(t, `{"iocs":[{
		"name":"projects/pid/locations/us/instances/cust/iocs/ioc_1",
		"displayName":"example.com",
		"iocType":"DOMAIN"
	}]}`)

	got, err := c.FetchRelatedIoCs(context.Background(), RelatedIoCQuery{
		IocType:          RelatedIoCDomain,
		ThreatCollection: "report--1",
		PageSize:         5,
		MaxPages:         1,
	})
	if err != nil {
		t.Fatalf("FetchRelatedIoCs: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ioc_1" || got[0].DisplayName != "example.com" {
		t.Fatalf("decoded IoCs wrong: %+v", got)
	}
	if !strings.Contains(rt.req.URL.Path, "/v1/projects/pid/locations/us/instances/cust/iocs:fetchRelated") {
		t.Fatalf("path = %s", rt.req.URL.Path)
	}
	q := rt.req.URL.Query()
	if q.Get("iocType") != "DOMAIN" {
		t.Errorf("iocType = %q", q.Get("iocType"))
	}
	wantTC := "projects/000000000000/locations/us/instances/cust/threatCollections/report--1"
	if q.Get("threatCollection") != wantTC {
		t.Errorf("threatCollection = %q, want %q", q.Get("threatCollection"), wantTC)
	}
	if q.Get("pageSize") != "5" {
		t.Errorf("pageSize = %q", q.Get("pageSize"))
	}
}
