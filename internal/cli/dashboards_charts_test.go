package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runDryRun executes a guarded dashboards-chart verb in --dry-run mode (no
// credentials needed) and returns its stdout. Delegates to the shared runCmd
// capture helper.
func runDryRun(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	out, err := runCmd(t, cmd, append(args, "--dry-run")...)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out
}

func TestDashboardsChartVerbsDryRun(t *testing.T) {
	// add-chart: dry-run previews the query and never touches the wire.
	out := runDryRun(t, newDashboardsAddChartCmd(),
		"db_1", "--title", "DNS by host", "--query", `metadata.event_type = "NETWORK_DNS"`)
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "NETWORK_DNS") {
		t.Errorf("add-chart dry-run output unexpected:\n%s", out)
	}

	// edit-chart: dry-run previews the replacement query.
	out = runDryRun(t, newDashboardsEditChartCmd(),
		"db_1", "--chart-id", "ch_1", "--query", `metadata.event_type = "USER_LOGIN"`)
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "USER_LOGIN") {
		t.Errorf("edit-chart dry-run output unexpected:\n%s", out)
	}

	// remove-chart: dry-run names the chart and dashboard.
	out = runDryRun(t, newDashboardsRemoveChartCmd(), "db_1", "--chart-id", "ch_1")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_1") {
		t.Errorf("remove-chart dry-run output unexpected:\n%s", out)
	}
}

// TestAddChartRequiresQueryForVisualization: a visualization chart with no query
// errors before any apply (the whole point of the verb is authoring a query).
func TestAddChartRequiresQueryForVisualization(t *testing.T) {
	cmd := newDashboardsAddChartCmd()
	cmd.SetArgs([]string{"db_1", "--title", "no query", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "needs a query") {
		t.Errorf("want a 'needs a query' error, got %v", err)
	}
}

// TestAddChartRejectsBadJSON: an invalid --datasource is rejected before apply.
func TestAddChartRejectsBadJSON(t *testing.T) {
	cmd := newDashboardsAddChartCmd()
	cmd.SetArgs([]string{"db_1", "--title", "t", "--query", "x", "--datasource", "{not json", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("want a JSON-validation error, got %v", err)
	}
}

func TestTileTypeToken(t *testing.T) {
	cases := map[string]string{
		"":              "TILE_TYPE_VISUALIZATION",
		"visualization": "TILE_TYPE_VISUALIZATION",
		"BUTTON":        "TILE_TYPE_BUTTON",
	}
	for in, want := range cases {
		got, err := tileTypeToken(in)
		if err != nil || got != want {
			t.Errorf("tileTypeToken(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"pie", "markdown"} {
		if _, err := tileTypeToken(bad); err == nil {
			t.Errorf("tileTypeToken(%q) should error", bad)
		}
	}
}

func TestReadChartQueryFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "q.yaral")
	if err := os.WriteFile(p, []byte("  metadata.event_type = \"NETWORK_DNS\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readChartQuery("", p)
	if err != nil {
		t.Fatal(err)
	}
	if got != `metadata.event_type = "NETWORK_DNS"` {
		t.Errorf("query-file content not trimmed/read: %q", got)
	}
}

func TestRawJSONOrNil(t *testing.T) {
	if v, err := rawJSONOrNil("x", "  "); v != nil || err != nil {
		t.Errorf("empty should be nil,nil; got %s,%v", v, err)
	}
	if _, err := rawJSONOrNil("x", "{bad"); err == nil {
		t.Error("invalid JSON should error")
	}
	if v, err := rawJSONOrNil("x", `{"a":1}`); err != nil || string(v) != `{"a":1}` {
		t.Errorf("valid JSON round-trip failed: %s,%v", v, err)
	}
}

func TestNestedString(t *testing.T) {
	raw := json.RawMessage(`{"chartDatasource":{"dashboardQuery":"projects/p/.../dashboardQueries/dq_9"}}`)
	if got := nestedString(raw, "chartDatasource", "dashboardQuery"); got != "projects/p/.../dashboardQueries/dq_9" {
		t.Errorf("nestedString = %q", got)
	}
	if got := nestedString(raw, "chartDatasource", "missing"); got != "" {
		t.Errorf("missing key should yield empty, got %q", got)
	}
}
