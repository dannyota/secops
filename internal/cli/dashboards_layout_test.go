package cli

import (
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestLayoutMoveDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsLayoutMoveCmd(),
		"db_1", "--widget-id", "ch_1", "--x", "10", "--span-x", "48")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_1") {
		t.Errorf("layout move dry-run output unexpected:\n%s", out)
	}
	if !strings.Contains(out, "x=10") || !strings.Contains(out, "w=48") {
		t.Errorf("layout move dry-run should list changed fields:\n%s", out)
	}
}

func TestLayoutMoveRequiresSomething(t *testing.T) {
	cmd := newDashboardsLayoutMoveCmd()
	cmd.SetArgs([]string{"db_1", "--widget-id", "ch_1", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "nothing to move") {
		t.Errorf("want 'nothing to move' error, got %v", err)
	}
}

func TestShortTileType(t *testing.T) {
	cases := map[string]string{
		chronicle.TileTypeVisualization: "CHART",
		chronicle.TileTypeButton:        "BUTTON",
		chronicle.TileTypeMarkdown:      "MARKDOWN",
		"UNKNOWN":                       "UNKNOWN",
	}
	for in, want := range cases {
		if got := shortTileType(in); got != want {
			t.Errorf("shortTileType(%q) = %q, want %q", in, got, want)
		}
	}
}
