// Package cli builds the secopsctl cobra command tree. Each subcommand lives in
// its own file and self-registers via init(), so adding a command never edits
// this file.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
)

// Global persistent-flag values, shared across subcommands.
var (
	cfgFile        string // --config
	jsonOut        bool   // --json
	forceLegacy    bool   // --legacy: force the legacy AppKey path, skip modern v1alpha
	nonInteractive bool   // --non-interactive: never prompt (no TTY confirmation)
)

var rootCmd = &cobra.Command{
	Use:   "secopsctl",
	Short: "Operate Google SecOps (Chronicle SIEM + Siemplify SOAR) as code",
	Long: "secopsctl operates a Google SecOps instance — Chronicle SIEM and\n" +
		"Siemplify SOAR — as code, for any tenant.\n\n" +
		"Config as code: pull live state -> review in `git diff` -> push back\n" +
		"(rules, feeds, parsers, dashboards, playbooks, webhooks, ...).\n" +
		"Live operations: UDM query, alerts, cases, per-alert triage verbs,\n" +
		"AI investigations and case summaries, playbook authoring and runs.\n\n" +
		"Every mutation is guarded: dry-run by default, --yes to apply, and a\n" +
		"hard read-only mode for automation (--read-only / SECOPS_READONLY=1).\n\n" +
		"Getting started: first run `secopsctl config` then `secopsctl doctor`.\n" +
		"Discover every command with `secopsctl commands`; every API surface with\n" +
		"`secopsctl surfaces`.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Command groups for the root help screen, so `secopsctl --help` renders titled
// sections instead of one flat list. Every top-level command is assigned a
// GroupID in its registration (or here for cobra's built-in help/completion).
const (
	groupSetup     = "setup"
	groupRead      = "read"
	groupAsCode    = "ascode"
	groupSOAR      = "soar"
	groupUtilities = "util"
)

// Execute runs the CLI and returns a process exit code: 0 success, 2 divergence
// (drift / would-change), 1 any other error.
func Execute() int {
	// Cobra adds `help` and `completion` lazily during Execute; materialize them
	// first so the guard below covers them too (else `completion bogus` exits 0).
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignCommandGroups(rootCmd)
	requireSubcommand(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		// Under --json a failure is emitted as a structured envelope on stdout so
		// an agent/script branches on {code,message,retryable,request_id} instead
		// of regexing the stderr prose. The exit code is unchanged.
		if jsonOut && renderErrorJSON(err) {
			var ec *exitCoder
			if errors.As(err, &ec) {
				return ec.ExitCode()
			}
			return 1
		}
		var ec *exitCoder
		if errors.As(err, &ec) {
			fmt.Fprintf(os.Stderr, "secopsctl: %v\n", err)
			return ec.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "secopsctl: error: %v\n", err)
		return 1
	}
	return 0
}

// helpOnlyParents marks the group parents whose RunE was injected by
// requireSubcommand (help-only, not real work) — so the `commands` catalog can
// tell a genuinely runnable parent (e.g. `info`) from a navigation group.
var helpOnlyParents = map[*cobra.Command]bool{}

// requireSubcommand makes every parent command (one that has subcommands and no
// run of its own) reject an unknown or extra argument, so a typo'd subcommand
// (`soar bogus`) exits non-zero instead of silently printing help with status 0.
// A bare parent (`soar`) still prints its help and exits 0.
func requireSubcommand(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		requireSubcommand(c)
	}
	if cmd.HasSubCommands() && cmd.Run == nil && cmd.RunE == nil {
		// cobra short-circuits a non-runnable parent to "print help, exit 0" BEFORE
		// validating args, so a typo'd subcommand passes. Giving it a RunE makes it
		// runnable so arg validation runs; the validator then reports
		// `unknown command "x"` — with a "Did you mean this?" suggestion.
		// (Set the validator even when Args is already set — e.g. cobra's own
		// `completion` group — since the miss is the non-runnable short-circuit.)
		if cmd.Args == nil {
			cmd.Args = rejectUnknownSubcommand
		}
		cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
		helpOnlyParents[cmd] = true
	}
}

// commandGroupByName maps each top-level command to a help group. Commands
// self-register from their own files, so the GroupID is assigned centrally here
// (after registration) rather than threaded through every newXxxCmd. A command
// missing from this map falls into Utilities, so cobra never warns about an
// ungrouped command.
var commandGroupByName = map[string]string{
	// Setup & health.
	"config": groupSetup, "doctor": groupSetup, "info": groupSetup, "version": groupSetup,
	"capabilities": groupSetup,
	// Read & query.
	"query": groupRead, "entity": groupRead, "rules": groupRead, "curated": groupRead,
	"alerts": groupRead, "cases": groupRead, "watchlists": groupRead, "ti": groupRead,
	"iocs": groupRead, "parsers": groupRead, "surfaces": groupRead, "commands": groupRead,
	// Config as code.
	"pull": groupAsCode, "push": groupAsCode, "drift": groupAsCode,
	"dashboards": groupAsCode, "reference_lists": groupAsCode,
	"rule_exclusions": groupAsCode, "feeds": groupAsCode, "pipeline": groupAsCode,
	// SOAR.
	"soar": groupSOAR,
	// Utilities.
	"cleanup": groupUtilities,
}

// assignCommandGroups stamps a GroupID onto every top-level command so the root
// help renders titled sections. cobra's built-in help/completion and anything
// not explicitly mapped fall into Utilities (cobra warns if a command has no
// group while groups exist).
func assignCommandGroups(root *cobra.Command) {
	for _, c := range root.Commands() {
		if g, ok := commandGroupByName[c.Name()]; ok {
			c.GroupID = g
		} else {
			c.GroupID = groupUtilities
		}
	}
}

// rejectUnknownSubcommand is the Args validator for help-only parent commands:
// any extra positional argument is a mistyped subcommand, so it errors with
// `unknown command "x"` — and, unlike cobra.NoArgs, appends the Levenshtein
// "Did you mean this?" suggestion (cobra omits suggestions from NoArgs, only
// the default legacyArgs path produces them). Mirrors cobra's own message.
func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "unknown command %q for %q", args[0], cmd.CommandPath())
	// cobra's own findSuggestions defaults the minimum distance to 2 before
	// matching; replicate that so a one-edit typo (pll -> pull) is suggested.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg.WriteString("\n\nDid you mean this?\n")
		for _, s := range suggestions {
			msg.WriteString("\t" + s + "\n")
		}
	}
	return fmt.Errorf("%s", msg.String())
}

func init() {
	cobra.OnInitialize(initViper)
	rootCmd.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "Setup & health:"},
		&cobra.Group{ID: groupRead, Title: "Read & query:"},
		&cobra.Group{ID: groupAsCode, Title: "Config as code (pull / review / push):"},
		&cobra.Group{ID: groupSOAR, Title: "SOAR:"},
		&cobra.Group{ID: groupUtilities, Title: "Utilities:"},
	)
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "",
		"path to the instance config YAML (overrides $SECOPSCTL_CONFIG and discovery)")
	pf.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON where supported")
	pf.BoolVar(&forceLegacy, "legacy", false,
		"force the legacy AppKey path on dual-generation surfaces (currently the 'soar case list' "+
			"surface); ignored where a command has no modern/legacy split")
	pf.BoolVar(&nonInteractive, "non-interactive", false,
		"never prompt; a guarded mutation without --yes is refused rather than asking")
	pf.BoolVar(&readOnlyFlag, "read-only", false,
		"hard read-only session: every guarded mutation degrades to a dry-run preview even with --yes "+
			"(also enabled by SECOPS_READONLY=1 — set it in the environment that launches an agent)")
}

func initViper() {
	viper.SetEnvPrefix("SECOPSCTL")
	viper.AutomaticEnv()
}

// loadInstance resolves and loads the instance config honoring --config.
func loadInstance() (*config.Instance, error) {
	return config.Load(cfgFile)
}

// newChronicleClient builds a Chronicle SIEM client from the resolved config.
// Credentials are OAuth/ADC, minted in-process by the Google auth library and
// resolved lazily on the first request.
func newChronicleClient() (*chronicle.Client, error) {
	inst, err := loadInstance()
	if err != nil {
		return nil, err
	}
	return chronicle.NewClient(inst.Settings(), auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4)))
}

// baseContext is the root context for API calls (placeholder for future
// signal-aware cancellation).
func baseContext() context.Context { return context.Background() }
