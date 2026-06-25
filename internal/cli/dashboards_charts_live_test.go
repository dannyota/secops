package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// execCLI runs a freshly-built leaf command, captures its stdout, and fails on
// error. A new command per call avoids cobra flag-state leaking between steps.
// Delegates to the shared runCmd capture helper.
func execCLI(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	out, err := runCmd(t, cmd, args...)
	if err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, out)
	}
	return out
}

// TestLiveDashboardsChartCLISmoke drives the dashboards chart verbs end to end
// through their real cobra RunE against a throwaway dashboard: add-chart (authors
// a YARA-L query) → charts (derefs it back) → edit-chart (replaces it) →
// remove-chart. Self-cleaning (t.Cleanup deletes the dashboard). Gated on
// SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveDashboardsChartCLISmoke(t *testing.T) {
	if os.Getenv("SECOPS_SIEM_SMOKE") != "1" {
		t.Skip("live SIEM smoke — set SECOPS_SIEM_SMOKE=1 (with instance config + ADC) to run")
	}
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (creates + deletes a throwaway dashboard)")
	}
	c, err := newChronicleClient()
	if err != nil {
		t.Skipf("no instance config/client: %v", err)
	}
	ctx := baseContext()

	label := fmt.Sprintf("secopsctl-smoketest-dashcli-%d", time.Now().UnixNano())
	dash, err := c.CreateDashboard(ctx, label, "secopsctl chart-cli smoke", chronicle.DashboardPrivate, nil, nil)
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashID := lastSegment(dash.Name)
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			if err := c.DeleteDashboard(ctx, dashID); err != nil {
				t.Logf("cleanup: delete dashboard %q: %v", label, err)
			}
		}
	})

	// add-chart authors the query.
	const query = "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.hostname\noutcome:\n  $c = count(metadata.id)"
	execCLI(t, newDashboardsAddChartCmd(), dashID, "--title", "DNS by host", "--query", query, "--yes")

	// Resolve the new chart id from the dashboard definition (charts are refs).
	chartID := firstChartID(t, c, dashID)
	if chartID == "" {
		t.Fatal("no chart in the dashboard after add-chart")
	}

	// charts derefs the chart back to its query.
	out := execCLI(t, newDashboardsChartsCmd(), dashID)
	if !strings.Contains(out, "NETWORK_DNS") {
		t.Errorf("charts output missing the authored query:\n%s", out)
	}

	// edit-chart replaces the query; re-deref must show the new text.
	const edited = "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.ip\noutcome:\n  $c = count(metadata.id)"
	execCLI(t, newDashboardsEditChartCmd(), dashID, "--chart-id", chartID, "--query", edited, "--yes")
	if out := execCLI(t, newDashboardsChartsCmd(), dashID); !strings.Contains(out, "principal.ip") {
		t.Errorf("charts output missing the edited query:\n%s", out)
	}

	// remove-chart detaches it; the definition must then carry no charts.
	execCLI(t, newDashboardsRemoveChartCmd(), dashID, "--chart-id", chartID, "--yes")
	if id := firstChartID(t, c, dashID); id != "" {
		t.Errorf("chart %s still on the dashboard after remove-chart", id)
	}

	if err := c.DeleteDashboard(ctx, dashID); err != nil {
		t.Fatalf("delete dashboard: %v", err)
	}
	deleted = true
}

// TestLiveDashboardsAuthoringSmoke drives the W82/W83 verbs end to end against a
// throwaway dashboard: add-chart --chart-type (generates+validates the
// visualization) → charts confirms the generated bar viz is stored → run-chart
// executes it → verify reports it → in-place edit-chart --chart-type table (clears
// viz) and --layout (definition.charts PATCH, other charts intact) → batch
// add-charts (idempotent skip on re-run). Self-cleaning. Same gate as above.
func TestLiveDashboardsAuthoringSmoke(t *testing.T) {
	if os.Getenv("SECOPS_SIEM_SMOKE") != "1" || os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1 (creates + deletes a throwaway dashboard)")
	}
	c, err := newChronicleClient()
	if err != nil {
		t.Skipf("no instance config/client: %v", err)
	}
	ctx := baseContext()

	label := fmt.Sprintf("secopsctl-smoketest-authoring-%d", time.Now().UnixNano())
	dash, err := c.CreateDashboard(ctx, label, "secopsctl authoring smoke", chronicle.DashboardPrivate, nil, nil)
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashID := lastSegment(dash.Name)
	t.Cleanup(func() {
		if err := c.DeleteDashboard(ctx, dashID); err != nil {
			t.Logf("cleanup: delete dashboard %q: %v", label, err)
		}
	})

	// add-chart --chart-type bar generates the visualization and validates --x/--y
	// against the query's match field (principal.hostname) and outcome var (c).
	const q = "metadata.event_type = \"NETWORK_DNS\"\nmatch:\n  principal.hostname\noutcome:\n  $c = count(metadata.id)"
	execCLI(t, newDashboardsAddChartCmd(), dashID, "--title", "bar-by-host",
		"--query", q, "--chart-type", "bar", "--x", "principal.hostname", "--y", "c", "--yes")

	chartID := firstChartID(t, c, dashID)
	if chartID == "" {
		t.Fatal("no chart after add-chart --chart-type bar")
	}

	// The generated visualization must be stored as a BAR series with the encode.
	chartRaw, err := c.GetChart(ctx, chartID)
	if err != nil {
		t.Fatalf("get chart: %v", err)
	}
	if viz := nestedRaw(chartRaw, "visualization"); !strings.Contains(string(viz), "BAR") {
		t.Errorf("generated visualization is not a BAR series:\n%s", viz)
	}

	// run-chart executes the chart's query (must not error; data optional).
	execCLI(t, newDashboardsRunChartCmd(), dashID, "--chart-id", chartID)
	// verify reports the dashboard (run-chart succeeded → not ERROR).
	if out, _ := runCmd(t, newDashboardsVerifyCmd(), dashID); strings.Contains(out, "ERROR") {
		t.Errorf("verify reports an ERROR chart on a healthy throwaway:\n%s", out)
	}

	// edit-chart --chart-type table clears the visualization in place (same id).
	execCLI(t, newDashboardsEditChartCmd(), dashID, "--chart-id", chartID, "--chart-type", "table", "--yes")
	if id := firstChartID(t, c, dashID); id != chartID {
		t.Errorf("chart id churned on in-place edit: %s -> %s", chartID, id)
	}

	// edit-chart --layout repositions via definition.charts; the chart survives.
	execCLI(t, newDashboardsEditChartCmd(), dashID, "--chart-id", chartID,
		"--layout", `{"startX":0,"spanX":48,"startY":0,"spanY":16}`, "--yes")
	if firstChartID(t, c, dashID) != chartID {
		t.Error("chart dropped by --layout edit")
	}

	// batch add-charts: idempotent — a second run with the same titles adds 0.
	batch := `[{"title":"batch-a","query":"` + jsonEscape(q) + `","chartType":"bar","x":"principal.hostname","y":"c"}]`
	bf := t.TempDir() + "/batch.json"
	if err := os.WriteFile(bf, []byte(batch), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := execCLI(t, newDashboardsAddChartsCmd(), dashID, "--file", bf, "--yes"); !strings.Contains(out, "1 added") {
		t.Errorf("batch first run should add 1:\n%s", out)
	}
	if out := execCLI(t, newDashboardsAddChartsCmd(), dashID, "--file", bf, "--yes"); !strings.Contains(out, "0 added") {
		t.Errorf("batch re-run should be idempotent (0 added):\n%s", out)
	}
}

// nestedRaw returns the raw JSON value at key from raw, or nil.
func nestedRaw(raw json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m[key]
}

// jsonEscape escapes a string for embedding inside a JSON string literal.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// firstChartID returns the bare id of the dashboard's first chart, or "".
func firstChartID(t *testing.T, c *chronicle.Client, dashID string) string {
	t.Helper()
	full, err := c.GetDashboard(baseContext(), dashID, true)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	var def struct {
		Definition struct {
			Charts []struct {
				DashboardChart string `json:"dashboardChart"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	if len(def.Definition.Charts) == 0 {
		return ""
	}
	return lastSegment(def.Definition.Charts[0].DashboardChart)
}
