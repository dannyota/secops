package cli

import "github.com/spf13/cobra"

// newIngestCmd groups the data-ingestion surfaces (the console's "SIEM Settings"
// ingestion area): feeds, forwarders, parsers, log types, the processing pipeline,
// and ingestion health. RBAC (`data-access`) stays its own top-level group, and the
// pull/push reconcile targets keep their snake_case names (they mirror the on-disk
// dirs) — only the imperative/read command groups moved under `ingest`.
func init() { rootCmd.AddCommand(newIngestCmd()) }

func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest <area>",
		Short: "Data ingestion: feeds, forwarders, parsers, log types, pipeline, health",
		Long: "Get logs into the SIEM and manage how they are parsed and processed:\n" +
			"  feeds       managed log feeds\n" +
			"  forwarders  forwarders + collection agents\n" +
			"  parsers     log parsers (run / validate / extensions)\n" +
			"  log-types   available log types\n" +
			"  pipeline    the data-processing pipeline\n" +
			"  health      ingestion-health watchdog (error-notification configs)\n\n" +
			"Config-as-code for these lives under `pull`/`push` with snake_case targets.",
	}
	cmd.AddCommand(
		newFeedsCmd(), newForwardersCmd(), newParsersCmd(),
		newLogTypesCmd(), newPipelineCmd(), newIngestionHealthCmd(),
	)
	return cmd
}
