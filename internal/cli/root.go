// Package cli builds the secopsctl cobra command tree. Each subcommand lives in
// its own file and self-registers via init(), so adding a command never edits
// this file.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	Short: "Operate Google SecOps (Chronicle) as code",
	Long: "secopsctl operates a Google SecOps (Chronicle) instance as code:\n" +
		"read-only pull, UDM query, and guarded mutating push, for any tenant.\n\n" +
		"Core loop: pull live state -> review in `git diff` -> push back.\n" +
		"Every push is a live production deploy and defaults to a dry run.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI and returns a process exit code: 0 success, 2 divergence
// (drift / would-change), 1 any other error.
func Execute() int {
	// Cobra adds `help` and `completion` lazily during Execute; materialize them
	// first so the guard below covers them too (else `completion bogus` exits 0).
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	requireSubcommand(rootCmd)
	if err := rootCmd.Execute(); err != nil {
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
		// runnable so arg validation runs; NoArgs then reports `unknown command "x"`.
		// (Set RunE even when Args is already NoArgs — e.g. cobra's own `completion`
		// group — since the miss is the non-runnable short-circuit, not the validator.)
		if cmd.Args == nil {
			cmd.Args = cobra.NoArgs
		}
		cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
	}
}

func init() {
	cobra.OnInitialize(initViper)
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "",
		"path to the instance config YAML (overrides $SECOPSCTL_CONFIG and discovery)")
	pf.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON where supported")
	pf.BoolVar(&forceLegacy, "legacy", false,
		"force the legacy AppKey path only, skipping the modern v1alpha API (for surfaces that support both)")
	pf.BoolVar(&nonInteractive, "non-interactive", false,
		"never prompt; a guarded mutation without --yes is refused rather than asking")
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
