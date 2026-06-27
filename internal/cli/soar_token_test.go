package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// withSOARTokenFlag sets the package-level --soar-token value for one test and
// restores it after, so cases don't leak into each other.
func withSOARTokenFlag(t *testing.T, v string) {
	t.Helper()
	prev := soarToken
	soarToken = v
	t.Cleanup(func() { soarToken = prev })
}

func TestResolveSOARToken(t *testing.T) {
	t.Run("none set", func(t *testing.T) {
		withSOARTokenFlag(t, "")
		t.Setenv("SECOPS_SOAR_TOKEN", "")
		got, err := resolveSOARToken()
		if err != nil || got != "" {
			t.Fatalf("got (%q, %v), want empty", got, err)
		}
	})

	t.Run("literal flag", func(t *testing.T) {
		withSOARTokenFlag(t, "jwt-literal")
		t.Setenv("SECOPS_SOAR_TOKEN", "")
		got, err := resolveSOARToken()
		if err != nil || got != "jwt-literal" {
			t.Fatalf("got (%q, %v), want jwt-literal", got, err)
		}
	})

	t.Run("env var fallback", func(t *testing.T) {
		withSOARTokenFlag(t, "")
		t.Setenv("SECOPS_SOAR_TOKEN", "jwt-from-env")
		got, err := resolveSOARToken()
		if err != nil || got != "jwt-from-env" {
			t.Fatalf("got (%q, %v), want jwt-from-env", got, err)
		}
	})

	t.Run("flag wins over env", func(t *testing.T) {
		withSOARTokenFlag(t, "jwt-flag")
		t.Setenv("SECOPS_SOAR_TOKEN", "jwt-env")
		got, err := resolveSOARToken()
		if err != nil || got != "jwt-flag" {
			t.Fatalf("got (%q, %v), want jwt-flag", got, err)
		}
	})

	t.Run("env: indirection", func(t *testing.T) {
		withSOARTokenFlag(t, "env:MY_JWT")
		t.Setenv("MY_JWT", "indirect-jwt")
		got, err := resolveSOARToken()
		if err != nil || got != "indirect-jwt" {
			t.Fatalf("got (%q, %v), want indirect-jwt", got, err)
		}
	})

	t.Run("env: indirection missing", func(t *testing.T) {
		withSOARTokenFlag(t, "env:UNSET_JWT")
		t.Setenv("UNSET_JWT", "")
		if _, err := resolveSOARToken(); err == nil {
			t.Fatal("expected error for unset env indirection")
		}
	})

	t.Run("@file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "tok.jwt")
		if err := os.WriteFile(p, []byte("  file-jwt\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		withSOARTokenFlag(t, "@"+p)
		got, err := resolveSOARToken()
		if err != nil || got != "file-jwt" {
			t.Fatalf("got (%q, %v), want file-jwt (trimmed)", got, err)
		}
	})

	t.Run("@file missing", func(t *testing.T) {
		withSOARTokenFlag(t, "@"+filepath.Join(t.TempDir(), "nope.jwt"))
		if _, err := resolveSOARToken(); err == nil {
			t.Fatal("expected error for missing @file")
		}
	})

	t.Run("@file empty", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "empty.jwt")
		if err := os.WriteFile(p, []byte("   \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		withSOARTokenFlag(t, "@"+p)
		if _, err := resolveSOARToken(); err == nil {
			t.Fatal("expected error for empty @file")
		}
	})
}
