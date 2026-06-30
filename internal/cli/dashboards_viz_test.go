package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestQueryVars(t *testing.T) {
	q := "metadata.log_type != \"\"\nmatch: $lt = metadata.log_type\noutcome: $c = count(metadata.id)\norder: $c desc"
	got := queryVars(q)
	if want := []string{"c", "lt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("queryVars = %v, want %v", got, want)
	}
}

func TestReservedQueryVars(t *testing.T) {
	// $rule and $events collide with reserved keywords; $rule_name and $c do not.
	q := "security_result.rule_name != \"\"\nmatch: $rule = security_result.rule_name\noutcome: $events = count(metadata.id)\n$rule_name = security_result.rule_name\n$c = count(metadata.id)"
	got := reservedQueryVars(q)
	if want := []string{"events", "rule"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reservedQueryVars = %v, want %v", got, want)
	}
	// Case-insensitive: $MATCH is reserved.
	if got := reservedQueryVars("outcome: $MATCH = count(x)"); !reflect.DeepEqual(got, []string{"MATCH"}) {
		t.Errorf("case-insensitive reserved check = %v, want [MATCH]", got)
	}
	// A clean aggregation has no reserved collisions.
	if got := reservedQueryVars("match: metadata.log_type\noutcome: $count = count(metadata.id)"); len(got) != 0 {
		t.Errorf("clean query flagged: %v", got)
	}
}

func TestValidateEncodeVars(t *testing.T) {
	q := "match: $endpoint = target.url\noutcome: $requests = count(metadata.id)"
	if err := validateEncodeVars(q, "endpoint", "requests"); err != nil {
		t.Errorf("valid aliased vars rejected: %v", err)
	}
	if err := validateEncodeVars(q, "endpoint", "typo"); err == nil {
		t.Error("a non-existent encode var should be rejected")
	}
	// A bare match-field reference (no $alias) is a valid column — accept it.
	bareQ := "metadata.event_type = \"NETWORK_CONNECTION\"\nmatch:\n  target.hostname\noutcome:\n  $count = count(metadata.id)"
	if err := validateEncodeVars(bareQ, "target.hostname", "count"); err != nil {
		t.Errorf("bare match field rejected: %v", err)
	}
	if err := validateEncodeVars(bareQ, "principal.hostname", "count"); err == nil {
		t.Error("a field not in the query should be rejected")
	}
	// A query with no aggregation variables is rejected for --chart-type.
	if err := validateEncodeVars(`metadata.event_type = "X"`, "x", "y"); err == nil {
		t.Error("a non-aggregation query should be rejected for --chart-type")
	}
}

func TestBuildVisualization(t *testing.T) {
	// bar
	b, err := buildVisualization("bar", "endpoint", "requests", "")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v["groupingType"] != "Off" {
		t.Errorf("bar groupingType = %v, want Off", v["groupingType"])
	}
	enc := v["series"].([]any)[0].(map[string]any)["encode"].(map[string]any)
	if enc["x"] != "endpoint" || enc["y"] != "requests" {
		t.Errorf("bar encode = %v", enc)
	}

	// stacked bar via series-by
	b, _ = buildVisualization("bar", "day", "count", "user")
	_ = json.Unmarshal(b, &v)
	if v["groupingType"] != "Stacked" {
		t.Errorf("series-by bar should be Stacked, got %v", v["groupingType"])
	}
	// FR-59: seriesColumn must be at viz top level, NOT inside series[0].encode
	sc, _ := v["seriesColumn"].([]any)
	if len(sc) != 1 || sc[0] != "user" {
		t.Errorf("seriesColumn should be [\"user\"] at top level, got %v", v["seriesColumn"])
	}
	enc = v["series"].([]any)[0].(map[string]any)["encode"].(map[string]any)
	if _, has := enc["seriesColumn"]; has {
		t.Error("seriesColumn must NOT be inside series[0].encode (FR-59)")
	}
	// FR-62: xAxes/yAxes must be present
	if v["xAxes"] == nil || v["yAxes"] == nil {
		t.Error("stacked bar must have xAxes and yAxes (FR-62)")
	}
	// FR-63: series entry must have encode, seriesType, dataLabel
	s0 := v["series"].([]any)[0].(map[string]any)
	if s0["seriesType"] != "BAR" {
		t.Errorf("series[0].seriesType = %v, want BAR", s0["seriesType"])
	}
	if s0["dataLabel"] == nil {
		t.Error("series[0] must have dataLabel (FR-63)")
	}
	if s0["stack"] != "stack" {
		t.Errorf("series[0].stack = %v, want \"stack\"", s0["stack"])
	}

	// pie uses itemName/value
	b, _ = buildVisualization("pie", "name", "count", "")
	_ = json.Unmarshal(b, &v)
	enc = v["series"].([]any)[0].(map[string]any)["encode"].(map[string]any)
	if enc["itemName"] != "name" || enc["value"] != "count" {
		t.Errorf("pie encode = %v", enc)
	}

	// table → no visualization
	if b, _ := buildVisualization("table", "", "", ""); b != nil {
		t.Errorf("table should produce no visualization, got %s", b)
	}

	// bad type
	if _, err := buildVisualization("bogus", "x", "y", ""); err == nil || !strings.Contains(err.Error(), "chart-type") {
		t.Errorf("bad chart-type should error, got %v", err)
	}
}

func TestPrepareChartInput(t *testing.T) {
	in, err := prepareChartInput(chartSpec{
		Title: "By host", Query: "match: $h = principal.hostname\noutcome: $c = count(metadata.id)",
		ChartType: "bar", X: "h", Y: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.DisplayName != "By host" || len(in.Visualization) == 0 || len(in.ChartLayout) == 0 {
		t.Errorf("prepareChartInput defaults missing: %+v", in)
	}
	// no title → error
	if _, err := prepareChartInput(chartSpec{Query: "x"}); err == nil {
		t.Error("spec without title should error")
	}
	// chartType + raw visualization → error
	if _, err := prepareChartInput(chartSpec{Title: "t", ChartType: "bar", X: "a", Y: "b", Visualization: json.RawMessage(`{}`)}); err == nil {
		t.Error("chartType + visualization should be mutually exclusive")
	}
}
