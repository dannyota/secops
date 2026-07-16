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

func TestFiltersSetApplyToDryRun(t *testing.T) {
	// --apply-to all
	out := runDryRun(t, newDashboardsFiltersSetCmd(),
		"db_1", "--time", "7", "--unit", "DAY", "--apply-to", "all")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "apply-to: all") {
		t.Errorf("filters set --apply-to all dry-run output unexpected:\n%s", out)
	}

	// --apply-to specific IDs
	out = runDryRun(t, newDashboardsFiltersSetCmd(),
		"db_1", "--time", "1", "--unit", "HOUR", "--apply-to", "ch_1,ch_2")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "apply-to: ch_1,ch_2") {
		t.Errorf("filters set --apply-to ids dry-run output unexpected:\n%s", out)
	}
}

func TestApplyFilterToCharts(t *testing.T) {
	charts := []json.RawMessage{
		json.RawMessage(`{"dashboardChart":"projects/p/instances/i/dashboardCharts/ch_1","filtersIds":[]}`),
		json.RawMessage(`{"dashboardChart":"projects/p/instances/i/dashboardCharts/ch_2"}`),
		json.RawMessage(`{"dashboardChart":"projects/p/instances/i/dashboardCharts/ch_3"}`),
	}

	// all: every chart gets filtersIds.
	out, err := applyFilterToCharts(charts, "all")
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range out {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("chart %d: %v", i, err)
		}
		var ids []string
		if err := json.Unmarshal(m["filtersIds"], &ids); err != nil {
			t.Fatalf("chart %d filtersIds unmarshal: %v", i, err)
		}
		if len(ids) != 1 || ids[0] != "GlobalTimeFilter" {
			t.Errorf("chart %d filtersIds = %v, want [GlobalTimeFilter]", i, ids)
		}
	}

	// specific IDs: only targeted charts are updated.
	out, err = applyFilterToCharts(charts, "ch_1,ch_3")
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range out {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("chart %d: %v", i, err)
		}
		seg := []string{"ch_1", "ch_2", "ch_3"}[i]
		_, hasFilter := m["filtersIds"]
		if seg == "ch_2" {
			// ch_2 was not targeted — original had no filtersIds key.
			if hasFilter {
				t.Errorf("chart ch_2 should not have filtersIds set")
			}
		} else {
			if !hasFilter {
				t.Errorf("chart %s should have filtersIds set", seg)
			}
		}
	}

	// no match: error.
	_, err = applyFilterToCharts(charts, "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "none of the specified") {
		t.Errorf("want no-match error, got %v", err)
	}

	// empty: error.
	_, err = applyFilterToCharts(charts, "  ,  ")
	if err == nil || !strings.Contains(err.Error(), "no chart IDs") {
		t.Errorf("want empty-IDs error, got %v", err)
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
