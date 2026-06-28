package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarkdownAddDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsMarkdownAddCmd(),
		"db_1", "--title", "Notes", "--text", "## Hello")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "Notes") {
		t.Errorf("markdown add dry-run output unexpected:\n%s", out)
	}
}

func TestMarkdownAddRequiresContent(t *testing.T) {
	cmd := newDashboardsMarkdownAddCmd()
	cmd.SetArgs([]string{"db_1", "--title", "Empty", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("want an 'empty' error, got %v", err)
	}
}

func TestMarkdownEditDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsMarkdownEditCmd(),
		"db_1", "--chart-id", "ch_md_1", "--text", "## Updated")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_md_1") {
		t.Errorf("markdown edit dry-run output unexpected:\n%s", out)
	}
}

func TestMarkdownEditRequiresSomething(t *testing.T) {
	cmd := newDashboardsMarkdownEditCmd()
	cmd.SetArgs([]string{"db_1", "--chart-id", "ch_md_1", "--yes"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "nothing to edit") {
		t.Errorf("want 'nothing to edit' error, got %v", err)
	}
}

func TestMarkdownRemoveDryRun(t *testing.T) {
	out := runDryRun(t, newDashboardsMarkdownRemoveCmd(),
		"db_1", "--chart-id", "ch_md_1")
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "ch_md_1") {
		t.Errorf("markdown remove dry-run output unexpected:\n%s", out)
	}
}

func TestMarkdownVisualization(t *testing.T) {
	vis := markdownVisualization("## Hello", "#f0f0f0")
	var parsed struct {
		Markdown struct {
			Content    string `json:"content"`
			Properties struct {
				BackgroundColor string `json:"backgroundColor"`
			} `json:"properties"`
		} `json:"markdown"`
	}
	if err := json.Unmarshal(vis, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Markdown.Content != "## Hello" {
		t.Errorf("content = %q, want %q", parsed.Markdown.Content, "## Hello")
	}
	if parsed.Markdown.Properties.BackgroundColor != "#f0f0f0" {
		t.Errorf("bgColor = %q, want %q", parsed.Markdown.Properties.BackgroundColor, "#f0f0f0")
	}

	visNoBg := markdownVisualization("text", "")
	if strings.Contains(string(visNoBg), "properties") {
		t.Error("properties should be omitted when bgColor is empty")
	}
}
