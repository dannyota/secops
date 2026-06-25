package mirror

import (
	"encoding/json"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

func dashObj(t *testing.T, body string) reconcile.Object {
	t.Helper()
	return reconcile.Object{Raw: json.RawMessage(body)}
}

// TestValidateDashboardObject covers the structural checks that catch an
// API-rejected body in the dry-run preview.
func TestValidateDashboardObject(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"ok minimal", `{"displayName":"D","definition":{}}`, false},
		{"missing displayName", `{"displayName":"","definition":{}}`, true},
		{"new chart with query ok", `{"displayName":"D","definition":{"charts":[{"query":"metadata.event_type=\"X\""}]}}`, false},
		{"new empty chart", `{"displayName":"D","definition":{"charts":[{"tileType":"TILE_TYPE_VISUALIZATION"}]}}`, true},
		{"reference chart needs no title", `{"displayName":"D","definition":{"charts":[{"_server":{"chart":"dc/1"}}]}}`, false},
		{"bad tileType", `{"displayName":"D","definition":{"charts":[{"title":"t","tileType":"BOGUS"}]}}`, true},
		{"non-object layout", `{"displayName":"D","definition":{"charts":[{"title":"t","chartLayout":[1,2]}]}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDashboardObject(dashObj(t, tc.body))
			if (err != nil) != tc.wantErr {
				t.Errorf("validateDashboardObject = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestServerChartConfigs asserts the definition.charts PATCH array is built only
// when every chart has a resolved id, and carries layout/filters.
func TestServerChartConfigs(t *testing.T) {
	withIDs := []desiredChart{
		{Title: "a", Server: &chartServer{Chart: "dc/1"}, ChartLayout: json.RawMessage(`{"startX":0}`)},
		{Title: "b", Server: &chartServer{Chart: "dc/2"}, FiltersIds: []string{"f1"}},
	}
	cfgs, ok := serverChartConfigs(withIDs)
	if !ok || len(cfgs) != 2 {
		t.Fatalf("serverChartConfigs ok=%v len=%d, want true/2", ok, len(cfgs))
	}
	// A chart without an id makes the whole array unbuildable (would drop a chart).
	if _, ok := serverChartConfigs([]desiredChart{{Title: "new"}}); ok {
		t.Error("serverChartConfigs should be !ok when a chart lacks an id")
	}
}

// TestChartConfigsDiffer covers reorder, layout, filters, and membership.
func TestChartConfigsDiffer(t *testing.T) {
	a := desiredChart{Server: &chartServer{Chart: "dc/1"}, ChartLayout: json.RawMessage(`{"startX":0}`)}
	b := desiredChart{Server: &chartServer{Chart: "dc/2"}}
	same := chartConfigsDiffer([]desiredChart{a, b}, []desiredChart{a, b})
	if same {
		t.Error("identical chart configs should not differ")
	}
	if !chartConfigsDiffer([]desiredChart{a, b}, []desiredChart{b, a}) {
		t.Error("reorder should differ")
	}
	aMoved := desiredChart{Server: &chartServer{Chart: "dc/1"}, ChartLayout: json.RawMessage(`{"startX":48}`)}
	if !chartConfigsDiffer([]desiredChart{aMoved, b}, []desiredChart{a, b}) {
		t.Error("layout change should differ")
	}
	if !chartConfigsDiffer([]desiredChart{a}, []desiredChart{a, b}) {
		t.Error("removal should differ")
	}
}
