package cli

import "testing"

// TestParsersDeleteRegistered verifies the delete command is wired into the
// parsers group.
func TestParsersDeleteRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"ingest", "parsers", "delete"})
	if err != nil || cmd == nil {
		t.Fatal("ingest parsers delete not registered")
	}
	if cmd.Use != "delete <log-type> <parser-id>" {
		t.Errorf("Use = %q, want delete command", cmd.Use)
	}
}

// TestParsersDeleteFlags verifies the guard flags exist and the command is
// marked as honoring the global --json flag.
func TestParsersDeleteFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"ingest", "parsers", "delete"})
	if cmd == nil {
		t.Fatal("command not found")
	}
	for _, name := range []string{"dry-run", "yes", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not found", name)
		}
	}
	if cmd.Annotations[jsonAnnotation] != "true" {
		t.Error("command not marked as honoring --json")
	}
}
