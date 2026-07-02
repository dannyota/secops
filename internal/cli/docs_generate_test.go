package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCollectDocGroupsCoversEveryRunnableOnce(t *testing.T) {
	want := map[string]bool{}
	walkRunnable(rootCmd, "", func(path string, _ *cobra.Command) { want[path] = false })
	if len(want) == 0 {
		t.Fatal("walkRunnable found no commands")
	}

	groups := collectDocGroups()
	for _, g := range groups {
		for _, e := range g.entries {
			seen, ok := want[e.path]
			if !ok {
				t.Errorf("group %s has unknown path %q", g.name, e.path)
			}
			if seen {
				t.Errorf("path %q appears in more than one group", e.path)
			}
			want[e.path] = true
		}
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("runnable command %q missing from every group", path)
		}
	}

	last := groups[len(groups)-1]
	if last.name != "global" {
		t.Fatalf("last group = %q, want global", last.name)
	}
	for _, e := range last.entries {
		if strings.Contains(e.path, " ") {
			t.Errorf("global group holds nested path %q", e.path)
		}
	}
}

func TestRenderCommandPages(t *testing.T) {
	pages := renderCommandPages(collectDocGroups())

	idx, ok := pages["README.md"]
	if !ok {
		t.Fatal("no index page generated")
	}
	if !strings.Contains(idx, "# Command reference") {
		t.Error("index missing title")
	}

	global, ok := pages["global.md"]
	if !ok {
		t.Fatal("no global page generated")
	}
	if !strings.Contains(global, "## doctor") {
		t.Error("global page missing `## doctor`")
	}

	soar, ok := pages["soar.md"]
	if !ok {
		t.Fatal("no soar page generated")
	}
	if !strings.Contains(soar, "```text\nsecopsctl soar") {
		t.Error("soar page missing a usage fence")
	}
	if !strings.Contains(soar, "> Guarded mutation — dry-run by default; apply with `--yes`.") {
		t.Error("soar page missing the guarded-mutation note")
	}

	// Every table row must be a single line ending in "|" — a stray newline or
	// unescaped pipe inside a cell would silently break the rendered table.
	for name, content := range pages {
		for line := range strings.SplitSeq(content, "\n") {
			if strings.HasPrefix(line, "| ") && !strings.HasSuffix(line, " |") {
				t.Errorf("%s: malformed table row %q", name, line)
			}
		}
	}
}

func TestTableCell(t *testing.T) {
	got := tableCell("a|b\nc\td")
	if want := `a\|b c d`; got != want {
		t.Errorf("tableCell = %q, want %q", got, want)
	}
}

func TestPatchSidebar(t *testing.T) {
	block := sidebarStartMarker + "\n- **Command reference**\n" + sidebarEndMarker

	replaced, err := patchSidebar("head\n"+sidebarStartMarker+"\nold\n"+sidebarEndMarker+"\ntail\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if replaced != "head\n"+block+"\ntail\n" {
		t.Errorf("marker replace got %q", replaced)
	}

	inserted, err := patchSidebar("- [Home](/)\n\n- **Design**\n  - [x](x.md)\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inserted, block+"\n\n- **Design**") {
		t.Errorf("insert-before-Design got %q", inserted)
	}

	appended, err := patchSidebar("- [Home](/)\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(appended, block+"\n") {
		t.Errorf("append got %q", appended)
	}

	if _, err := patchSidebar("x "+sidebarStartMarker+" y", block); err == nil {
		t.Error("start marker without end marker should error")
	}
}

func TestWriteAndCheckCommandDocs(t *testing.T) {
	dir := t.TempDir()
	sidebar := filepath.Join(dir, "_sidebar.md")
	if err := os.WriteFile(sidebar, []byte("- [Home](/)\n\n- **Design**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "commands")
	pages := renderCommandPages(collectDocGroups())

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := writeCommandDocs(cmd, pages, out, sidebar); err != nil {
		t.Fatal(err)
	}
	if err := checkCommandDocs(pages, out, sidebar); err != nil {
		t.Fatalf("check after write: %v", err)
	}

	// A stale page, an orphaned page, and a stale sidebar must all fail --check.
	if err := os.WriteFile(filepath.Join(out, "global.md"), []byte("outdated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "renamed-group.md"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkCommandDocs(pages, out, sidebar)
	if err == nil {
		t.Fatal("check passed on stale docs")
	}
	for _, want := range []string{"stale: global.md", "orphaned: renamed-group.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("check error missing %q: %v", want, err)
		}
	}

	// A rerun of write must heal both.
	if err := writeCommandDocs(cmd, pages, out, sidebar); err != nil {
		t.Fatal(err)
	}
	if err := checkCommandDocs(pages, out, sidebar); err != nil {
		t.Fatalf("check after heal: %v", err)
	}
}
