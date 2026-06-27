package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestLeafHelpResolvesToLeaf pins the fix for the reported regression where a
// leaf verb's `--help` fell through to the root help instead of the verb's own
// usage. For a representative set of leaves across both planes, `<path> --help`
// must print a Usage line naming the full command path — not the root's.
func TestLeafHelpResolvesToLeaf(t *testing.T) {
	leaves := [][]string{
		{"rules", "list"},
		{"rules", "curated", "set"},
		{"search", "udm"},
		{"alerts", "get"},
		{"alerts", "update"},
		{"cases", "close"},
		{"soar", "settings", "grouping", "set"},
	}
	for _, path := range leaves {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetErr(&buf)
			rootCmd.SetArgs(append(append([]string{}, path...), "--help"))
			t.Cleanup(func() { rootCmd.SetArgs(nil); rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("help for %v errored: %v", path, err)
			}
			out := buf.String()
			wantUsage := "secopsctl " + strings.Join(path, " ")
			if !strings.Contains(out, wantUsage) {
				t.Errorf("help for %v did not name its own usage %q; got:\n%s", path, wantUsage, out)
			}
		})
	}
}
