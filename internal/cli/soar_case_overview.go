package cli

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

// newCaseOverviewCmd surfaces the data behind the console's case Overview tab:
// the case's entities (with their enrichment) by default, or the overview widget
// template with --widgets. Read-only, over the legacy external API
// (case-overview/GetCaseEntities and GetCaseOverviewData). Completes the case
// read trio alongside `cases summarize` (AI narrative) and `cases get`
// (record + alerts).
func newCaseOverviewCmd() *cobra.Command {
	var caseID int
	var widgets bool
	cmd := &cobra.Command{
		Use:   "overview --id N [--widgets]",
		Short: "Read-only: show a case's overview entities (or --widgets template) — the console Overview tab",
		Long: "Return the data behind the console's case Overview tab. By default this is the\n" +
			"case's entities with their enrichment — the entity context an analyst sees.\n" +
			"--widgets returns the overview widget template (the configured layout) instead.\n" +
			"Output is JSON. Complements `cases summarize` (the AI narrative) and `cases get`\n" +
			"(the record + its alerts).",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			var raw json.RawMessage
			if widgets {
				raw, err = c.CaseOverviewGetData(ctx, map[string]any{"caseId": caseID})
			} else {
				raw, err = c.CaseOverviewGetCaseEntities(ctx, caseID)
			}
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().IntVar(&caseID, "id", 0, "SOAR case id (required)")
	cmd.Flags().BoolVar(&widgets, "widgets", false, "return the overview widget template instead of the case entities")
	_ = cmd.MarkFlagRequired("id")
	return markJSON(cmd)
}
