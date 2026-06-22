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
