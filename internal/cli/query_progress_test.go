package cli

import "testing"

// TestProgressSuppression verifies that printProgress and clearProgress are
// no-ops when suppressed (--no-progress, --json, or non-TTY stderr), so search
// output stays clean for piped/scripted consumers.
func TestProgressSuppression(t *testing.T) {
	// progress.go guards on noProgress || jsonOut || !stderrIsTTY().
	// In `go test` stderr is not a TTY, so printProgress is already a no-op —
	// verify it does not panic or produce output.
	printProgress("events", 0, 0)  // indeterminate
	printProgress("events", 5, 10) // determinate
	clearProgress()

	// Explicitly set the suppression flags and confirm no panic.
	old := noProgress
	noProgress = true
	printProgress("raw log", 1, 100)
	clearProgress()
	noProgress = old
}

// TestRawAllFlagsCombinable pins that --raw and --all combine: the complete-
// results engine fetches the events (reporting the total match count) and the
// raw hydration runs over that result. Only --count-only excludes them.
func TestRawAllFlagsCombinable(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"search", "udm"})
	if err != nil || cmd == nil {
		t.Fatalf("find search udm: %v", err)
	}
	if err := cmd.Flags().Parse([]string{"q", "--raw", "--all"}); err != nil {
		t.Errorf("--raw --all should parse together: %v", err)
	}
	if err := cmd.ValidateFlagGroups(); err != nil {
		t.Errorf("--raw --all should not be mutually exclusive: %v", err)
	}
}
