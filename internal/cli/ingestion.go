package cli

import "github.com/spf13/cobra"

// newIngestionHealthCmd surfaces ingestion-health signals — the error-notification
// configs that watch for delayed/zero-ingesting/erroring log sources. Registered
// directly under `ingest` (ingest.go) as `ingest health`. Read-only.
func newIngestionHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Read-only: list the error-notification configs that watch log-source health",
		Long: "List the error-notification configs — the thresholds that flag delayed,\n" +
			"zero-ingesting, or erroring log sources (the ingestion-health watchdog). JSON.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			cfgs, err := c.ListErrorNotificationConfigs(baseContext())
			if err != nil {
				return err
			}
			return emitJSON(cfgs)
		},
	}
	return markJSON(cmd)
}
