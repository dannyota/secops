package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyMode(t *testing.T) {
	t.Setenv("SECOPS_READONLY", "")
	if readOnlyMode() {
		t.Fatal("read-only must be off by default")
	}
	// Fail-closed: any value other than an explicit falsy enables the cap —
	// a mis-spelled truthy must never silently leave a session write-capable.
	for _, v := range []string{"1", "true", "YES", "on", "enabled", "y", "anything"} {
		t.Setenv("SECOPS_READONLY", v)
		if !readOnlyMode() {
			t.Errorf("SECOPS_READONLY=%s must enable read-only mode (fail closed)", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off", "  "} {
		t.Setenv("SECOPS_READONLY", v)
		if readOnlyMode() {
			t.Errorf("SECOPS_READONLY=%q must not enable read-only mode", v)
		}
	}
	readOnlyFlag = true
	t.Cleanup(func() { readOnlyFlag = false })
	if !readOnlyMode() {
		t.Error("--read-only flag must enable read-only mode")
	}
}

func TestSOARGuardReadOnlyDegrades(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "1")
	dr, ay := soarGuard("test action", false, true) // --yes passed
	if !dr || ay {
		t.Errorf("read-only soarGuard(--yes) = dryRun %v, assumeYes %v; want true, false", dr, ay)
	}
}

func TestAuditMutationWritesJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SECOPSCTL_HOME", home)
	auditMutation("close case 1 (reason=Maintenance)", "confirmed")
	auditMutation("push rules-deploy", "read-only")

	path := filepath.Join(home, "audit.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("audit log not written: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %o, want 0600", perm)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 records, got %d:\n%s", len(lines), b)
	}
	var rec struct {
		Time     string `json:"time"`
		Action   string `json:"action"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("record not JSON: %v", err)
	}
	if rec.Action != "close case 1 (reason=Maintenance)" || rec.Decision != "confirmed" || rec.Time == "" {
		t.Errorf("unexpected record: %+v", rec)
	}
}

func TestCommandsCatalog(t *testing.T) {
	rows := collectCommands(rootCmd, "")
	if len(rows) < 100 {
		t.Fatalf("suspiciously few commands: %d", len(rows))
	}
	byPath := map[string]commandRow{}
	for _, r := range rows {
		if _, dup := byPath[r.Path]; dup {
			t.Errorf("duplicate command path %q", r.Path)
		}
		byPath[r.Path] = r
	}
	// The kind heuristic: the --dry-run/--yes pair marks a guarded live mutation.
	// `info` is a runnable parent (it has subcommands AND real work of its own)
	// and must appear; navigation-only parents must not.
	wantKind := map[string]string{
		"alerts list":     "read",
		"alerts update":   "guarded-mutation",
		"soar case get":   "read",
		"soar case close": "guarded-mutation",
		"pull":            "read",
		"push":            "guarded-mutation",
		"commands":        "read",
		"info":            "read",
	}
	for path, want := range wantKind {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if r.Kind != want {
			t.Errorf("%q kind = %q, want %q", path, r.Kind, want)
		}
		if r.Short == "" {
			t.Errorf("%q has no short description", path)
		}
	}
	// Group parents must not appear (they are navigation, not verbs).
	for _, parent := range []string{"soar", "soar case", "rules"} {
		if _, ok := byPath[parent]; ok {
			t.Errorf("group parent %q must not be a catalog row", parent)
		}
	}
}

// TestGuardFlagPairInvariant asserts the convention everything else keys off:
// a command that can apply a live mutation (--yes) always carries the
// --dry-run half of the gate too. A future verb with a bare --yes would be
// misclassified as `read` by the catalog AND uncovered by read-only mode — this
// is the tripwire.
func TestGuardFlagPairInvariant(t *testing.T) {
	for _, r := range collectCommands(rootCmd, "") {
		hasYes, hasDry := false, false
		for _, f := range r.Flags {
			switch f.Name {
			case "yes":
				hasYes = true
			case "dry-run":
				hasDry = true
			}
		}
		if hasYes != hasDry {
			t.Errorf("%q has yes=%v dry-run=%v — the guard pair must travel together", r.Path, hasYes, hasDry)
		}
	}
}
