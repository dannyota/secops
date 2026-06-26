package cli

import "github.com/spf13/cobra"

// The `coverage` command reports MITRE ATT&CK detection coverage — the API-side
// view of which threat-collection × rule combinations cover which techniques.
// Read-only; a posture/scorecard input for detection engineers and SOC managers.
func init() { rootCmd.AddCommand(newCoverageCmd()) }

func newCoverageCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Read-only: MITRE ATT&CK detection coverage (threat-collection × rule)",
		Long: "Report detection coverage details (coverageDetails) — the per threat-\n" +
			"collection × rule MITRE ATT&CK coverage the platform computes: which\n" +
			"techniques deployed rules cover. JSON output; a coverage-posture input for\n" +
			"detection engineering and SOC reporting.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rows, err := c.ListCoverageDetails(baseContext(), limit)
			if err != nil {
				return err
			}
			return emitJSON(rows)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 1000, "max coverage rows to return")
	return markJSON(cmd)
}
