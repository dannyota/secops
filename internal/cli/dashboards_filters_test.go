package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFiltersSetDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsFiltersSetCmd(),
		"db_1", "--time", "7", "--unit", "DAY")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "7 DAY") {
		t.Errorf("filters set dry-run output unexpected:\n%s", out)
	}
}

func TestFiltersSetRejectsInvalidUnit(t *testing.T) {
	cmd := newDashboardsFiltersSetCmd()
	cmd.SetArgs([]string{"db_1", "--time", "7", "--unit", "YEAR", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid --unit") {
		t.Errorf("want unit validation error, got %v", err)
	}
}

func TestCreateDashboardDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsCreateCmd(),
		"--name", "Test Dashboard", "--access", "public")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "Test Dashboard") {
		t.Errorf("create dry-run output unexpected:\n%s", out)
	}
}

func TestCreateDashboardRejectsInvalidAccess(t *testing.T) {
	cmd := newDashboardsCreateCmd()
	cmd.SetArgs([]string{"--name", "X", "--access", "invalid", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid --access") {
		t.Errorf("want access validation error, got %v", err)
	}
}

func TestParseDashboardFilters(t *testing.T) {
	raw := json.RawMessage(`{"definition":{"filters":[
		{"id":"GlobalTimeFilter","isStandardTimeRangeFilter":true,
		 "filterOperatorAndFieldValues":[{"fieldValues":["14","DAY"]}]},
		{"id":"custom_1","filterOperatorAndFieldValues":[{"fieldValues":["value1"]}]}
	]}}`)
	filters := parseDashboardFilters(raw)
	if len(filters) != 2 {
		t.Fatalf("got %d filters, want 2", len(filters))
	}
	if !filters[0].IsTimeRange || filters[0].Summary != "14 DAY" {
		t.Errorf("filter 0: isTimeRange=%v summary=%q", filters[0].IsTimeRange, filters[0].Summary)
	}
	if filters[1].IsTimeRange || filters[1].Summary != "value1" {
		t.Errorf("filter 1: isTimeRange=%v summary=%q", filters[1].IsTimeRange, filters[1].Summary)
	}
}
