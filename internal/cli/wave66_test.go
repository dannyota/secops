package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Wave 66 — CLI UX polish. Offline tests for the behavioral changes: enum-error
// wording + verbatim echo, --hours<=0 rejection, the --case-id/--id alias both
// working, the --legacy flag rendering without a value token, and cobra
// "Did you mean" suggestions being enabled.

// runCmd executes a freshly-built command with args, capturing both the cobra
// out/err streams AND os.Stdout (the guarded-mutation banners write directly to
// os.Stdout) so the returned string is the full combined output and the test
// log stays clean. SilenceUsage/Errors keep cobra from re-printing the error.
func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	// Redirect os.Stdout for the duration of Execute, then merge it into buf.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = orig
	buf.WriteString(<-done)
	return buf.String(), err
}

// Item 4: an invalid alerts-update enum quotes the OPERATOR'S verbatim input
// (not the transformed wire token) and lists the valid set, matching the
// `soar case close --reason` gold standard.
func TestAlertsUpdateEnumErrorVerbatimAndValidSet(t *testing.T) {
	cases := []struct {
		flag, val string
		want      []string // substrings the error must contain
	}{
		{"--reason", "totallywrong", []string{`"totallywrong"`, "not-malicious", "malicious", "maintenance"}},
		{"--status", "bogus", []string{`"bogus"`, "new|reviewed|closed|open"}},
		{"--verdict", "nope", []string{`"nope"`, "true-positive|false-positive"}},
		{"--priority", "huge", []string{`"huge"`, "info|low|medium|high|critical"}},
		{"--reputation", "meh", []string{`"meh"`, "useful|not-useful"}},
	}
	for _, tc := range cases {
		out, err := runCmd(t, newAlertsUpdateCmd(), "alert-1", tc.flag, tc.val, "--dry-run")
		if err == nil {
			t.Fatalf("%s %s: expected an error, got nil (out=%q)", tc.flag, tc.val, out)
		}
		msg := err.Error()
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s %s: error %q must contain %q", tc.flag, tc.val, msg, w)
			}
		}
		// The transformed wire token must NOT leak into the message.
		if strings.Contains(msg, "REASON_") || strings.Contains(msg, "PRIORITY_") {
			t.Errorf("%s %s: error leaks a wire token: %q", tc.flag, tc.val, msg)
		}
	}
}

// A valid short enum value passes the CLI-side check (it then reaches the
// guard, which dry-runs without a client). "informative" is accepted as a
// priority synonym but is omitted from the hint.
func TestAlertsUpdateEnumAcceptsValid(t *testing.T) {
	// A valid value passes the enum check and reaches the guard, which dry-runs
	// to success (the dry-run banner is printed to os.Stdout, not captured here —
	// success is signalled by a nil error).
	out, err := runCmd(t, newAlertsUpdateCmd(), "alert-1", "--reason", "not-malicious", "--dry-run")
	if err != nil {
		t.Fatalf("valid reason should pass, got %v (out=%q)", err, out)
	}
	if got := alertEnumHint("priority"); strings.Contains(got, "informative") {
		t.Errorf("priority hint must omit the 'informative' synonym, got %q", got)
	}
}

// Item 7: windowed commands reject --hours <= 0 before any work.
func TestHoursMustBePositive(t *testing.T) {
	for _, h := range []int{0, -1, -24} {
		if err := checkHours(h); err == nil {
			t.Errorf("checkHours(%d) must error", h)
		} else if !strings.Contains(err.Error(), "positive number of hours") {
			t.Errorf("checkHours(%d) message = %q", h, err.Error())
		}
	}
	if err := checkHours(1); err != nil {
		t.Errorf("checkHours(1) must pass, got %v", err)
	}

	// End-to-end on a windowed command: search udm --hours 0 fails fast, before
	// any client is constructed.
	out, err := runCmd(t, newAlertsListCmd(), "--hours", "0")
	if err == nil || !strings.Contains(err.Error(), "positive number of hours") {
		t.Errorf("alerts list --hours 0 must reject; got err=%v out=%q", err, out)
	}
}

// Item 8: --id is the primary case-id flag on run-action and context set, with
// --case-id a hidden alias; both spellings set the same value.
func TestCaseIDFlagAliasBothWork(t *testing.T) {
	type builder struct {
		name string
		make func() *cobra.Command
		args func(idFlag string) []string
	}
	builders := []builder{
		{
			name: "run-action",
			make: newCaseRunActionCmd,
			args: func(idFlag string) []string {
				return []string{idFlag, "7", "--action", "Ping", "--instance", "uuid-1", "--dry-run"}
			},
		},
		{
			name: "context set",
			make: newCaseContextSetCmd,
			args: func(idFlag string) []string {
				return []string{idFlag, "7", "--key", "k", "--value", "v", "--dry-run"}
			},
		},
	}
	for _, b := range builders {
		for _, idFlag := range []string{"--id", "--case-id"} {
			// A nil error + dry-run preview proves the flag set caseID (>0):
			// otherwise the RunE short-circuits with "--id is required".
			out, err := runCmd(t, b.make(), b.args(idFlag)...)
			if err != nil {
				t.Errorf("%s %s: unexpected error %v (out=%q)", b.name, idFlag, err, out)
			}
			if !strings.Contains(out, "DRY RUN") {
				t.Errorf("%s %s: expected a dry-run preview, got %q", b.name, idFlag, out)
			}
		}
		// --case-id must be hidden, --id visible.
		cmd := b.make()
		if f := cmd.Flags().Lookup("case-id"); f == nil || !f.Hidden {
			t.Errorf("%s: --case-id must exist and be hidden", b.name)
		}
		if f := cmd.Flags().Lookup("id"); f == nil || f.Hidden {
			t.Errorf("%s: --id must exist and be visible", b.name)
		}
	}
}

// Item 9 / 10: deprecated aliases stay hidden, primaries visible.
func TestDeprecatedFlagAliasesHidden(t *testing.T) {
	// alerts list: --filter primary, --query hidden alias.
	al := newAlertsListCmd()
	if f := al.Flags().Lookup("filter"); f == nil || f.Hidden {
		t.Error("alerts list --filter must exist and be visible")
	}
	if f := al.Flags().Lookup("query"); f == nil || !f.Hidden {
		t.Error("alerts list --query must exist and be hidden")
	}
	// integration uninstall: --key primary, --name hidden alias.
	un := newSOARIntegrationUninstallCmd()
	if f := un.Flags().Lookup("key"); f == nil || f.Hidden {
		t.Error("integration uninstall --key must exist and be visible")
	}
	if f := un.Flags().Lookup("name"); f == nil || !f.Hidden {
		t.Error("integration uninstall --name must exist and be hidden")
	}
}

// Item 3: the global --legacy flag renders as a plain bool — no value token in
// its usage line (the example surface lives in the usage string, not as the
// placeholder).
func TestLegacyFlagRendersWithoutValueToken(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("legacy")
	if f == nil {
		t.Fatal("--legacy persistent flag not registered")
	}
	if f.Value.Type() != "bool" {
		t.Errorf("--legacy must be a bool flag, got type %q", f.Value.Type())
	}
	// cobra derives the value placeholder from the first backtick-quoted word in
	// the usage string; a bool with none renders no value token.
	name, usage := pflag.UnquoteUsage(f)
	if name != "" {
		t.Errorf("--legacy must render with no value placeholder, got %q", name)
	}
	if !strings.Contains(usage, "legacy AppKey path") {
		t.Errorf("--legacy usage lost its description: %q", usage)
	}
}

// Item 2: cobra "Did you mean" suggestions are enabled — a one-edit typo on a
// top-level command is suggested, and suggestions are not globally disabled.
func TestCobraSuggestionsEnabled(t *testing.T) {
	if rootCmd.DisableSuggestions {
		t.Fatal("rootCmd.DisableSuggestions must be false")
	}
	// rejectUnknownSubcommand is the validator help-only parents get; it must
	// error AND suggest the near-match.
	err := rejectUnknownSubcommand(rootCmd, []string{"pll"})
	if err == nil {
		t.Fatal("a typo'd command must error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "pll"`) {
		t.Errorf("missing unknown-command message: %q", msg)
	}
	if !strings.Contains(msg, "Did you mean this?") || !strings.Contains(msg, "pull") {
		t.Errorf("expected a 'Did you mean ... pull' suggestion, got %q", msg)
	}
	// No extra args is fine (a bare parent prints help).
	if err := rejectUnknownSubcommand(rootCmd, nil); err != nil {
		t.Errorf("no-args must not error, got %v", err)
	}
}

// Item 16: every top-level command lands in a help group after assignment, so
// cobra never warns about an ungrouped command.
func TestCommandGroupsAssigned(t *testing.T) {
	assignCommandGroups(rootCmd)
	groups := map[string]bool{}
	for _, g := range rootCmd.Groups() {
		groups[g.ID] = true
	}
	if len(groups) == 0 {
		t.Fatal("no command groups registered on root")
	}
	for _, c := range rootCmd.Commands() {
		if c.GroupID == "" {
			t.Errorf("command %q has no GroupID", c.Name())
			continue
		}
		if !groups[c.GroupID] {
			t.Errorf("command %q has unregistered GroupID %q", c.Name(), c.GroupID)
		}
	}
}

// Item 5: curated set validates --precision before constructing a client or
// printing the LIVE banner, so a bad value fails fast.
func TestCuratedSetPrecisionFailsFast(t *testing.T) {
	out, err := runCmd(t, newCuratedSetCmd(),
		"--category", "C", "--ruleset", "R", "--precision", "BOGUS", "--enabled")
	if err == nil || !strings.Contains(err.Error(), "precision") {
		t.Fatalf("bad --precision must fail fast; got err=%v out=%q", err, out)
	}
	// The LIVE banner must NOT have printed before the validation error.
	if strings.Contains(out, "LIVE SIEM change") {
		t.Errorf("precision must be validated BEFORE the guard banner; out=%q", out)
	}
}
