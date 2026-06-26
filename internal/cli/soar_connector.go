package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// The `soar connector` command holds imperative connector ops outside the
// pull/push reconcile loop: runtime statistics for one connector instance, and
// a guarded on-demand run to verify a connector pulls events after a config
// change.
func newSOARConnectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connector <verb>",
		Short: "Inspect connector runtime stats (read) and trigger an on-demand run (guarded)",
		Long: "Connector ops beside the reconcile loop:\n" +
			"  stat <identifier> — runtime statistics for one connector instance (read-only);\n" +
			"  run — trigger a connector instance to pull on demand (guarded).\n" +
			"Connector config-as-code is `soar pull connectors` / `soar push connectors`.",
	}
	cmd.AddCommand(newConnectorStatCmd(), newConnectorRunCmd())
	return cmd
}

func newConnectorStatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stat <identifier>",
		Short: "Read-only: runtime statistics for one connector instance (events, errors, last run)",
		Long: "Fetch runtime statistics for a connector instance by its identifier (from\n" +
			"`soar pull connectors`) — events processed, errors, and last-run timing, to\n" +
			"confirm a connector is healthy after a config change. JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := c.GetConnectorStatistics(baseContext(), args[0])
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	return markJSON(cmd)
}

func newConnectorRunCmd() *cobra.Command {
	var integration, connector, instance string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "run --integration X --connector Y --instance Z",
		Short: "MUTATING (guarded): trigger a connector instance to pull on demand",
		Long: "Run a connector instance once on demand — verify it pulls events after a\n" +
			"config change without waiting for its schedule. The three ids come from the\n" +
			"connector definition (`soar pull connectors`). Guarded: dry-run by default,\n" +
			"--yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			action := fmt.Sprintf("run connector %s/%s instance %s on demand", integration, connector, instance)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newSOARClient()
				if err != nil {
					return err
				}
				_, err = c.RunConnectorInstanceOnDemand(baseContext(), integration, connector, instance)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration id (required)")
	f.StringVar(&connector, "connector", "", "connector definition id (required)")
	f.StringVar(&instance, "instance", "", "connector instance id (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("connector")
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
}
