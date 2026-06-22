package chronicle_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveDashboardChartAuthoringWriteSmoke proves that secopsctl can author a
// chart's YARA-L query end to end, and pins the structural fact behind the
// dashboards "chart-reference-only" pull: the query does NOT live in the
// dashboard's definition.charts (that array references charts by resource name),
// it lives in a separate dashboardQueries resource that only :addChart / :editChart
// can author. The flow on a labeled throwaway dashboard:
//
//	create dashboard → :addChart (inline dashboardQuery) → get chart → get query
//	(assert the YARA-L round-trips) → assert the query text is NOT in the
//	dashboard definition (reference-only) → :editChart (change the query) →
//	:removeChart → delete dashboard.
//
// Self-cleaning (t.Cleanup deletes the dashboard, which cascades its chart +
// query). Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveDashboardChartAuthoringWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (creates + deletes a throwaway dashboard)")
	}

	label := fmt.Sprintf("secopsctl-smoketest-dash-%d", time.Now().UnixNano())
	dash, err := c.CreateDashboard(ctx, label, "secopsctl chart-authoring smoke", chronicle.DashboardPrivate, nil, nil)
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashID := lastSeg(dash.Name)
	if dashID == "" {
		t.Fatalf("created dashboard has no id (name=%q)", dash.Name)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if err := c.DeleteDashboard(ctx, dashID); err != nil {
			t.Logf("cleanup: delete throwaway dashboard %q: %v", label, err)
		}
	})

	const query = "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.hostname\noutcome:\n  $dns_query_count = count(metadata.id)\norder:\n  principal.hostname asc"

	addResp, err := c.AddChart(ctx, dashID, chronicle.AddChartInput{
		DisplayName:     "DNS queries by host",
		ChartLayout:     json.RawMessage(`{"startX":0,"spanX":12,"startY":0,"spanY":8}`),
		ChartDatasource: json.RawMessage(`{"dataSources":["UDM"]}`),
		Query:           query,
		Interval:        json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`),
	})
	if err != nil {
		t.Fatalf("addChart: %v", err)
	}

	chartName := jsonStr(addResp, "dashboardChart", "name")
	if chartName == "" {
		t.Fatalf("addChart response has no dashboardChart.name:\n%s", addResp)
	}

	// The created chart references a dashboardQuery resource — fetch the chart,
	// then the query, and assert the YARA-L we authored round-trips.
	chart, err := c.GetChart(ctx, chartName)
	if err != nil {
		t.Fatalf("getChart: %v", err)
	}
	queryRef := jsonStr(chart, "chartDatasource", "dashboardQuery")
	if queryRef == "" {
		t.Fatalf("chart has no chartDatasource.dashboardQuery ref:\n%s", chart)
	}
	q, err := c.GetQuery(ctx, queryRef)
	if err != nil {
		t.Fatalf("getQuery: %v", err)
	}
	if got := jsonStr(q, "query"); !strings.Contains(got, "NETWORK_DNS") {
		t.Errorf("authored query did not round-trip: getQuery.query = %q", got)
	}

	// Reference-only proof: the dashboard's own definition carries the chart
	// REFERENCE (resource name) but never the query text.
	full, err := c.GetDashboard(ctx, dashID, true)
	if err != nil {
		t.Fatalf("getDashboard full: %v", err)
	}
	defJSON := string(full.Raw)
	if !strings.Contains(defJSON, lastSeg(chartName)) {
		t.Errorf("dashboard definition does not reference the new chart id %q", lastSeg(chartName))
	}
	if strings.Contains(defJSON, "NETWORK_DNS") {
		t.Errorf("query text leaked into the dashboard definition — it should be reference-only:\n%s", defJSON)
	}

	// Edit the query through :editChart and confirm the change lands.
	const edited = "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.ip\noutcome:\n  $dns_query_count = count(metadata.id)\norder:\n  principal.ip asc"
	editBody, _ := json.Marshal(map[string]any{
		"name":  queryRef,
		"query": edited,
		"input": json.RawMessage(`{"relativeTime":{"timeUnit":"DAY","startTimeVal":"1"}}`),
		"etag":  jsonStr(q, "etag"),
	})
	if _, err := c.EditChart(ctx, dashID, chronicle.EditChartInput{DashboardQuery: editBody}); err != nil {
		t.Fatalf("editChart: %v", err)
	}
	q2, err := c.GetQuery(ctx, queryRef)
	if err != nil {
		t.Fatalf("getQuery after edit: %v", err)
	}
	if got := jsonStr(q2, "query"); !strings.Contains(got, "principal.ip") {
		t.Errorf("query edit did not apply: getQuery.query = %q", got)
	}

	// Remove the chart, then delete the dashboard, then confirm the chart
	// resource is actually gone — a charted dashboard spans separate
	// dashboardChart/dashboardQuery resources, so an orphan that survives cleanup
	// would silently violate the leave-no-residue rule. Surface it loudly instead.
	if _, err := c.RemoveChart(ctx, dashID, chartName); err != nil {
		t.Fatalf("removeChart: %v", err)
	}
	if err := c.DeleteDashboard(ctx, dashID); err != nil {
		t.Fatalf("delete dashboard: %v", err)
	}
	deleted = true
	if _, err := c.GetChart(ctx, chartName); err == nil {
		t.Errorf("chart %q still present after removeChart + dashboard delete — orphaned, needs manual cleanup", chartName)
	} else if !chronicle.IsNotFound(err) {
		t.Logf("post-cleanup GetChart returned a non-404 error (chart likely gone): %v", err)
	}
}

// lastSeg returns the trailing path segment of a resource name (or the input if
// it has no slash).
func lastSeg(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// jsonStr reads a nested string field from raw JSON by walking the given keys;
// it returns "" if any segment is missing or not a string.
func jsonStr(raw json.RawMessage, keys ...string) string {
	cur := raw
	for i, k := range keys {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(cur, &m); err != nil {
			return ""
		}
		v, ok := m[k]
		if !ok {
			return ""
		}
		if i == len(keys)-1 {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return ""
			}
			return s
		}
		cur = v
	}
	return ""
}
