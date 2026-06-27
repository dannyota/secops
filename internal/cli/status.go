package cli

import "github.com/spf13/cobra"

// newStatusCmd groups the read-only inspection/diagnostic commands: what the tool
// can do on this instance (capabilities), UDM/log coverage (coverage), and the
// API-surface map (surfaces). `drift` stays top-level — it is part of the pull/push
// config-as-code loop (it compares committed local state vs live), not a diagnostic.
func init() { rootCmd.AddCommand(newStatusCmd()) }

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <area>",
		Short: "Read-only diagnostics: capabilities, coverage, surfaces",
		Long: "Read-only inspection of the tool + instance:\n" +
			"  capabilities  session bootstrap — version + auth health + surface status\n" +
			"  coverage      UDM / log-type coverage of the ingested data\n" +
			"  surfaces      the API surface-family map (plane, version, lane, prune)",
	}
	cmd.AddCommand(newCapabilitiesCmd(), newCoverageCmd(), newSurfacesCmd())
	return cmd
}
