package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestButtonAddDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsButtonAddCmd(),
		"db_1", "--title", "Docs", "--label", "Go to docs", "--url", "https://example.com")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "Docs") {
		t.Errorf("button add dry-run output unexpected:\n%s", out)
	}
}

func TestButtonAddRequiresLabelAndURL(t *testing.T) {
	cmd := newDashboardsButtonAddCmd()
	cmd.SetArgs([]string{"db_1", "--title", "t", "--label", "l", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--url") {
		t.Errorf("want --url required error, got %v", err)
	}

	cmd2 := newDashboardsButtonAddCmd()
	cmd2.SetArgs([]string{"db_1", "--title", "t", "--url", "u", "--yes"})
	cmd2.SilenceUsage, cmd2.SilenceErrors = true, true
	if err := cmd2.Execute(); err == nil || !strings.Contains(err.Error(), "--label") {
		t.Errorf("want --label required error, got %v", err)
	}
}

func TestButtonEditDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsButtonEditCmd(),
		"db_1", "--chart-id", "ch_btn_1", "--label", "New label")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_btn_1") {
		t.Errorf("button edit dry-run output unexpected:\n%s", out)
	}
}

func TestButtonRemoveDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsButtonRemoveCmd(),
		"db_1", "--chart-id", "ch_btn_1")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_btn_1") {
		t.Errorf("button remove dry-run output unexpected:\n%s", out)
	}
}

func TestButtonVisualization(t *testing.T) {
	vis := buttonVisualization("Click me", "https://example.com", "desc", "#4285f4", "filled", true)
	var parsed struct {
		Button struct {
			Label      string `json:"label"`
			Hyperlink  string `json:"hyperlink"`
			NewTab     bool   `json:"newTab"`
			Properties struct {
				Color       string `json:"color"`
				ButtonStyle string `json:"buttonStyle"`
			} `json:"properties"`
		} `json:"button"`
	}
	if err := json.Unmarshal(vis, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Button.Label != "Click me" {
		t.Errorf("label = %q", parsed.Button.Label)
	}
	if parsed.Button.Hyperlink != "https://example.com" {
		t.Errorf("hyperlink = %q", parsed.Button.Hyperlink)
	}
	if !parsed.Button.NewTab {
		t.Error("newTab should be true")
	}
	if parsed.Button.Properties.ButtonStyle != "BUTTON_STYLE_FILLED" {
		t.Errorf("style = %q", parsed.Button.Properties.ButtonStyle)
	}
}

func TestButtonStyleToken(t *testing.T) {
	cases := map[string]string{
		"filled":      "BUTTON_STYLE_FILLED",
		"outlined":    "BUTTON_STYLE_OUTLINED",
		"transparent": "BUTTON_STYLE_TRANSPARENT",
		"unknown":     "unknown",
	}
	for in, want := range cases {
		if got := buttonStyleToken(in); got != want {
			t.Errorf("buttonStyleToken(%q) = %q, want %q", in, got, want)
		}
	}
}
