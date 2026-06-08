package cli

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// TestDivergenceExitCode locks the git-style code: a divergence is exit 2.
func TestDivergenceExitCode(t *testing.T) {
	var ec *exitCoder
	if !errors.As(divergence("x %d", 1), &ec) {
		t.Fatal("divergence() is not an *exitCoder")
	}
	if ec.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", ec.ExitCode())
	}
}

func newTestParent() *cobra.Command {
	p := &cobra.Command{Use: "parent", SilenceUsage: true, SilenceErrors: true}
	p.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
	requireSubcommand(p)
	p.SetOut(io.Discard)
	p.SetErr(io.Discard)
	return p
}

// TestRequireSubcommandRejectsUnknown: a typo'd subcommand exits non-zero.
func TestRequireSubcommandRejectsUnknown(t *testing.T) {
	p := newTestParent()
	p.SetArgs([]string{"bogus"})
	if err := p.Execute(); err == nil {
		t.Error("unknown subcommand should return an error (non-zero exit)")
	}
}

// TestRequireSubcommandBareParentOK: a bare parent prints help, no error.
func TestRequireSubcommandBareParentOK(t *testing.T) {
	p := newTestParent()
	p.SetArgs(nil)
	if err := p.Execute(); err != nil {
		t.Errorf("bare parent should print help without error, got %v", err)
	}
}

// TestRequireSubcommandKnownChildOK: a real subcommand still runs.
func TestRequireSubcommandKnownChildOK(t *testing.T) {
	p := newTestParent()
	p.SetArgs([]string{"child"})
	if err := p.Execute(); err != nil {
		t.Errorf("known subcommand should run, got %v", err)
	}
}

// TestEnsureDataDir: a live push into a missing data dir is refused; dry-run is allowed.
func TestEnsureDataDir(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDataDir("t", dir, false); err != nil {
		t.Errorf("existing dir, live: unexpected error %v", err)
	}
	missing := filepath.Join(dir, "nope")
	if err := ensureDataDir("t", missing, false); err == nil {
		t.Error("missing dir, live push: expected an error")
	}
	if err := ensureDataDir("t", missing, true); err != nil {
		t.Errorf("missing dir, dry-run: should be allowed, got %v", err)
	}
}

// TestConfirmPushNonInteractive: --non-interactive never auto-confirms.
func TestConfirmPushNonInteractive(t *testing.T) {
	old := nonInteractive
	defer func() { nonInteractive = old }()
	nonInteractive = true
	if confirmPush("t") {
		t.Error("--non-interactive must not auto-confirm a guarded mutation")
	}
}

// TestConfirmPushJSON: --json must not prompt (the y/N would corrupt stdout JSON).
func TestConfirmPushJSON(t *testing.T) {
	old := jsonOut
	defer func() { jsonOut = old }()
	jsonOut = true
	if confirmPush("t") {
		t.Error("--json must not prompt for confirmation")
	}
}
