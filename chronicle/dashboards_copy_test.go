package chronicle

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSplitDatasource pulls the embedded query reference out and preserves every
// other datasource field (dataSources AND any unmodeled config), dropping only the
// dashboardQuery ref so the copy gets a fresh query.
func TestSplitDatasource(t *testing.T) {
	raw := json.RawMessage(`{"dashboardQuery":"projects/p/locations/l/instances/i/dashboardQueries/old","dataSources":["UDM"],"customCfg":{"k":1}}`)
	ref, rest, err := splitDatasource(raw)
	if err != nil {
		t.Fatalf("splitDatasource: %v", err)
	}
	if ref != "projects/p/locations/l/instances/i/dashboardQueries/old" {
		t.Errorf("query ref = %q", ref)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rest, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["dashboardQuery"]; ok {
		t.Errorf("rest must drop dashboardQuery: %s", rest)
	}
	if string(m["dataSources"]) != `["UDM"]` || string(m["customCfg"]) != `{"k":1}` {
		t.Errorf("rest must preserve other fields: %s", rest)
	}
	// Empty datasource passes through.
	if r, _, err := splitDatasource(nil); err != nil || r != "" {
		t.Errorf("nil datasource: ref=%q err=%v", r, err)
	}
}

// TestAssembleChartInput verifies a recreated chart carries the source's
// display/type/visualization + layout + (already-split) datasource, and replays
// the query inline so the server mints a fresh query.
func TestAssembleChartInput(t *testing.T) {
	ref := copyChartRef{
		DashboardChart: "projects/p/locations/l/instances/i/dashboardCharts/old",
		ChartLayout:    json.RawMessage(`{"startX":0,"spanX":48,"startY":0,"spanY":24}`),
	}
	ch := copyChartBody{DisplayName: "Log Volume", Description: "d", TileType: "TILE_TYPE_VISUALIZATION", Visualization: json.RawMessage(`{"series":[]}`)}
	ds := json.RawMessage(`{"dataSources":["UDM"]}`)
	q := &copyQueryBody{Query: "metadata.log_type != \"\"", Input: json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`)}

	in, err := assembleChartInput(ref, ch, ds, q)
	if err != nil {
		t.Fatalf("assembleChartInput: %v", err)
	}
	if in.DisplayName != "Log Volume" || in.TileType != "TILE_TYPE_VISUALIZATION" {
		t.Errorf("display/type not copied: %+v", in)
	}
	if string(in.ChartLayout) != string(ref.ChartLayout) || string(in.Visualization) != `{"series":[]}` || string(in.ChartDatasource) != string(ds) {
		t.Errorf("layout/visualization/datasource not copied: %+v", in)
	}
	if in.Query != q.Query || string(in.Interval) != string(q.Input) {
		t.Errorf("query/interval not replayed inline: %q / %s", in.Query, in.Interval)
	}
}

// TestAssembleChartInputNoQuery: a chart without a query (e.g. a button tile)
// leaves Query/Interval empty rather than erroring.
func TestAssembleChartInputNoQuery(t *testing.T) {
	in, err := assembleChartInput(copyChartRef{ChartLayout: json.RawMessage(`{}`)}, copyChartBody{DisplayName: "Btn", TileType: "TILE_TYPE_BUTTON"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.Query != "" || in.Interval != nil {
		t.Errorf("no-query chart should have empty query/interval: %+v", in)
	}
}

// TestAssembleChartInputQueryNoInterval: a query with text but no input interval
// can't be sent faithfully (AddChart needs both) — reject loudly, never silently
// drop the query.
func TestAssembleChartInputQueryNoInterval(t *testing.T) {
	q := &copyQueryBody{Query: "metadata.log_type != \"\""} // no Input
	_, err := assembleChartInput(copyChartRef{}, copyChartBody{DisplayName: "X"}, nil, q)
	if err == nil || !strings.Contains(err.Error(), "no input interval") {
		t.Errorf("expected a clear no-interval error, got %v", err)
	}
}
