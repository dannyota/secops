package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeValidConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "instance.yaml")
	body := "project_id: p\nproject_number: \"1\"\nregion: us\ncustomer_id: c\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestExplicitConfigMissingFailsLoud: an explicitly named --config that does not
// exist must error, never fall through to discovery (wrong-tenant guard).
func TestExplicitConfigMissingFailsLoud(t *testing.T) {
	t.Setenv("SECOPSCTL_CONFIG", "")
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if _, err := Load(missing); err == nil {
		t.Fatal("expected error for a missing --config path")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should say the path was not found", err)
	}
}

// TestSourcePathSet: a loaded instance records the file it came from.
func TestSourcePathSet(t *testing.T) {
	p := writeValidConfig(t)
	inst, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if inst.SourcePath() != p {
		t.Errorf("SourcePath() = %q, want %q", inst.SourcePath(), p)
	}
}

// TestResolvedSource: returns the active file, and enforces explicit-exists.
func TestResolvedSource(t *testing.T) {
	t.Setenv("SECOPSCTL_CONFIG", "")
	p := writeValidConfig(t)
	got, err := ResolvedSource(p)
	if err != nil {
		t.Fatalf("ResolvedSource(%q): %v", p, err)
	}
	if got != p {
		t.Errorf("ResolvedSource = %q, want %q", got, p)
	}
	if _, err := ResolvedSource(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("ResolvedSource(missing) should error")
	}
}
