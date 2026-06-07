// Package cli builds the secopsctl cobra command tree. Each subcommand lives in
// its own file and self-registers via init(), so adding a command never edits
// this file.
package cli

import (
	"context"
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
	cfgFile     string // --config
	jsonOut     bool   // --json
	forceLegacy bool   // --legacy: force the legacy AppKey path, skip modern v1alpha
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

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "secopsctl: error: %v\n", err)
		return 1
	}
	return 0
}

func init() {
	cobra.OnInitialize(initViper)
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "",
		"path to the instance config YAML (overrides $SECOPSCTL_CONFIG and discovery)")
	pf.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON where supported")
	pf.BoolVar(&forceLegacy, "legacy", false,
		"force the legacy AppKey path only, skipping the modern v1alpha API (for surfaces that support both)")
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
