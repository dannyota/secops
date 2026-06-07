package cli

import (
	"io"
	"strings"
	"testing"
)

// TestLegacyCallPOSTGuard: a POST on the legacy API can read OR write, so the
// escape hatch must refuse a bare POST (no --read/--write) rather than run it
// ungated — a forgotten --write on a write-POST would otherwise deploy live.
func TestLegacyCallPOSTGuard(t *testing.T) {
	cmd := newSOARLegacyCallCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"cases/SomeOp", "--method", "POST"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("bare POST should be refused (needs --read or --write)")
	}
	if !strings.Contains(err.Error(), "read OR write") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestLegacyCallReadWriteExclusive: --read and --write cannot be combined.
func TestLegacyCallReadWriteExclusive(t *testing.T) {
	cmd := newSOARLegacyCallCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"cases/SomeOp", "--method", "POST", "--read", "--write"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("--read --write together should be rejected")
	}
}

func TestNormalizeSOARURL(t *testing.T) {
	cases := map[string]string{
		"tenant.siemplify-soar.com":          "https://tenant.siemplify-soar.com",
		"  tenant.siemplify-soar.com  ":      "https://tenant.siemplify-soar.com",
		"https://tenant.siemplify-soar.com/": "https://tenant.siemplify-soar.com",
		"https://tenant.example.com":         "https://tenant.example.com",
		"http://localhost:8080":              "http://localhost:8080",
		"":                                   "",
	}
	for in, want := range cases {
		if got := normalizeSOARURL(in); got != want {
			t.Errorf("normalizeSOARURL(%q) = %q, want %q", in, got, want)
		}
	}
}
