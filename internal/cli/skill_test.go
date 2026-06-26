package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSkillsDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if got := defaultSkillsDir(); got != filepath.Join("/cfg", "skills") {
		t.Fatalf("with CLAUDE_CONFIG_DIR: got %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := defaultSkillsDir(); filepath.Base(got) != "skills" {
		t.Fatalf("fallback: got %q, want a .../skills path", got)
	}
}

func TestSkillInstallWritesFile(t *testing.T) {
	dir := t.TempDir()
	install := func(args ...string) error {
		cmd := newSkillInstallCmd()
		cmd.SetArgs(append([]string{"--dir", dir}, args...))
		return cmd.Execute()
	}
	if err := install(); err != nil {
		t.Fatalf("install: %v", err)
	}
	dest := filepath.Join(dir, "secopsctl", "SKILL.md")
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("installed skill looks too small: %d bytes", len(b))
	}

	// Idempotent: re-installing the identical file is a clean no-op.
	if err := install(); err != nil {
		t.Fatalf("re-install (unchanged): %v", err)
	}

	// A hand-edited copy must not be silently clobbered without --force.
	if err := os.WriteFile(dest, []byte("# hand-tuned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install(); err == nil {
		t.Fatal("install over a differing file should refuse without --force")
	}
	if got, _ := os.ReadFile(dest); string(got) != "# hand-tuned\n" {
		t.Fatal("refused install must leave the existing file untouched")
	}
	// --force overwrites it back to the embedded guide.
	if err := install("--force"); err != nil {
		t.Fatalf("install --force: %v", err)
	}
	if got, _ := os.ReadFile(dest); len(got) < 1000 {
		t.Fatal("--force did not restore the embedded guide")
	}
}
