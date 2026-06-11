package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The authoring `update` verb must reject a body whose `name` is empty — that
// field (not displayName) is the create-vs-update marker, so an empty name
// would silently CREATE a duplicate. Offline; the guard runs before any client
// construction would matter.
func TestAuthoringUpdateRequiresResourceName(t *testing.T) {
	dir := t.TempDir()
	// displayName present, name empty (the create-template shape) — must error.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"name":"","displayName":"X","integration":"HTTP"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update", "--integration", "HTTP", "--file", bad, "--yes"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("empty name must be refused as a create-not-update, got %v", err)
	}

	// Missing required flags.
	cmd = newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("update without flags must error, got %v", err)
	}
}

func TestResourceNameOf(t *testing.T) {
	if got := resourceNameOf([]byte(`{"name":"projects/p/.../actions/9","displayName":"X"}`)); got == "" {
		t.Error("populated name must be returned")
	}
	if got := resourceNameOf([]byte(`{"name":"  ","displayName":"X"}`)); got != "" {
		t.Errorf("whitespace name must be empty, got %q", got)
	}
	if got := resourceNameOf([]byte(`{"displayName":"X"}`)); got != "" {
		t.Errorf("absent name must be empty, got %q", got)
	}
}
