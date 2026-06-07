package cli

import (
	"strings"
	"testing"
)

// TestResolveBuildInfoFallback: with no ldflags stamping, resolveBuildInfo never
// returns empty version/commit (it falls back to embedded VCS info, else "dev"/
// "unknown") and always fills the runtime fields.
func TestResolveBuildInfoFallback(t *testing.T) {
	bi := resolveBuildInfo()
	if bi.Version == "" {
		t.Error("version must never be empty")
	}
	if bi.Commit == "" {
		t.Error("commit must never be empty")
	}
	if bi.GoVersion == "" || bi.OS == "" || bi.Arch == "" {
		t.Errorf("runtime fields unset: %+v", bi)
	}
}

// TestShortCommit trims long SHAs and leaves short ids alone.
func TestShortCommit(t *testing.T) {
	if got := shortCommit("0123456789abcdef0123"); got != "0123456789ab" {
		t.Errorf("shortCommit long = %q, want 12 chars", got)
	}
	if got := shortCommit("abc"); got != "abc" {
		t.Errorf("shortCommit short = %q, want unchanged", got)
	}
	if got := shortCommit("unknown"); got != "unknown" {
		t.Errorf("shortCommit(unknown) = %q", got)
	}
}

// TestVersionLine is a compact, single-line, non-empty string.
func TestVersionLine(t *testing.T) {
	l := versionLine()
	if !strings.HasPrefix(l, "secopsctl ") || strings.Contains(l, "\n") {
		t.Errorf("versionLine malformed: %q", l)
	}
}
