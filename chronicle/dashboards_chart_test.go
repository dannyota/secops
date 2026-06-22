package chronicle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestAddChartBody: AddChart with an inline query posts to :addChart with the
// dashboardChart sub-object, chartLayout, and a dashboardQuery carrying the
// YARA-L query + input interval. This is the only path that authors a chart
// query — the dashboard definition.charts array references charts by name, it
// never carries the query text.
func TestAddChartBody(t *testing.T) {
	rt := &cannedRT{resp: `{"nativeDashboard":{},"dashboardChart":{"name":"ch_1"}}`}
	c := newCannedClient(t, rt)

	in := AddChartInput{
		DisplayName:     "DNS query metrics",
		ChartLayout:     json.RawMessage(`{"startX":0,"spanX":12,"startY":0,"spanY":8}`),
		ChartDatasource: json.RawMessage(`{"dataSources":["UDM"]}`),
		Query:           "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.hostname\noutcome:\n  $c = count(metadata.id)",
		Interval:        json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`),
	}
	if _, err := c.AddChart(context.Background(), "db_1", in); err != nil {
		t.Fatalf("AddChart: %v", err)
	}

	if !strings.Contains(rt.url, "/nativeDashboards/db_1:addChart") {
		t.Errorf("url = %q, want …/nativeDashboards/db_1:addChart", rt.url)
	}

	var got struct {
		DashboardChart struct {
			DisplayName     string          `json:"displayName"`
			TileType        string          `json:"tileType"`
			ChartDatasource json.RawMessage `json:"chartDatasource"`
		} `json:"dashboardChart"`
		ChartLayout    json.RawMessage `json:"chartLayout"`
		DashboardQuery *struct {
			Query string          `json:"query"`
			Input json.RawMessage `json:"input"`
		} `json:"dashboardQuery"`
	}
	if err := json.Unmarshal([]byte(rt.body), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rt.body)
	}
	if got.DashboardChart.DisplayName != "DNS query metrics" {
		t.Errorf("displayName = %q", got.DashboardChart.DisplayName)
	}
	if got.DashboardChart.TileType != TileTypeVisualization {
		t.Errorf("tileType = %q, want %q (default)", got.DashboardChart.TileType, TileTypeVisualization)
	}
	if len(got.ChartLayout) == 0 {
		t.Error("chartLayout missing from request body")
	}
	if got.DashboardQuery == nil {
		t.Fatal("dashboardQuery missing — the YARA-L query was not authored into the request")
	}
	if !strings.Contains(got.DashboardQuery.Query, "NETWORK_DNS") {
		t.Errorf("dashboardQuery.query = %q, want the YARA-L body", got.DashboardQuery.Query)
	}
	if !strings.Contains(string(got.DashboardQuery.Input), "relativeTime") {
		t.Errorf("dashboardQuery.input = %s, want the interval", got.DashboardQuery.Input)
	}
}

// TestAddChartOmitsQueryWithoutBothHalves: a dashboardQuery is attached only when
// BOTH query and interval are present — query-without-interval and
// interval-without-query each attach none.
func TestAddChartOmitsQueryWithoutBothHalves(t *testing.T) {
	layout := json.RawMessage(`{"startX":0,"spanX":4,"startY":0,"spanY":2}`)
	cases := map[string]AddChartInput{
		"query without interval": {DisplayName: "q-only", ChartLayout: layout, Query: `metadata.event_type = "NETWORK_DNS"`},
		"interval without query": {DisplayName: "i-only", ChartLayout: layout, Interval: json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`)},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			rt := &cannedRT{resp: `{}`}
			c := newCannedClient(t, rt)
			if _, err := c.AddChart(context.Background(), "db_1", in); err != nil {
				t.Fatalf("AddChart: %v", err)
			}
			if strings.Contains(rt.body, "dashboardQuery") {
				t.Errorf("dashboardQuery present without both halves:\n%s", rt.body)
			}
		})
	}
}

// TestEditChartQueryBody: EditChart editing a query qualifies the bare query name
// against the instance and sets an editMask of dashboard_query.query.
func TestEditChartQueryBody(t *testing.T) {
	rt := &cannedRT{resp: `{}`}
	c := newCannedClient(t, rt)
	in := EditChartInput{
		DashboardQuery: json.RawMessage(`{"name":"dq_1","query":"metadata.event_type = \"USER_LOGIN\"","etag":"e1"}`),
	}
	if _, err := c.EditChart(context.Background(), "db_1", in); err != nil {
		t.Fatalf("EditChart: %v", err)
	}

	var got struct {
		DashboardQuery struct {
			Name  string `json:"name"`
			Query string `json:"query"`
			Etag  string `json:"etag"`
		} `json:"dashboardQuery"`
		EditMask string `json:"editMask"`
	}
	if err := json.Unmarshal([]byte(rt.body), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rt.body)
	}
	wantName := "projects/pid/locations/us/instances/cust/dashboardQueries/dq_1"
	if got.DashboardQuery.Name != wantName {
		t.Errorf("query name = %q, want qualified %q", got.DashboardQuery.Name, wantName)
	}
	if got.DashboardQuery.Etag != "e1" {
		t.Errorf("etag = %q, want round-tripped e1", got.DashboardQuery.Etag)
	}
	if !strings.Contains(got.EditMask, "dashboard_query.query") {
		t.Errorf("editMask = %q, want dashboard_query.query", got.EditMask)
	}
}
