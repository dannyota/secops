package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// `alerts investigate` — the per-alert AI (TIN) investigation flow the web UI
// runs: trigger an investigation for one alert, poll it to completion, and
// report the verdict (false positive?, confidence, summary, next steps). The
// trigger starts a generation server-side, so it is refused in read-only mode;
// --latest is the read-only variant that reports the alert's most recent
// investigation instead of starting a new one.

func newAlertsInvestigateCmd() *cobra.Command {
	var latest bool
	cmd := &cobra.Command{
		Use:   "investigate <alert-id>",
		Short: "Run the AI investigation for one alert and report the verdict (starts a generation; --latest reads)",
		Long: "Trigger the AI (Gemini) investigation for one detection alert and poll until\n" +
			"it completes, then print the verdict, confidence, summary, and suggested next\n" +
			"steps. Completion typically takes a minute or two. Starting an investigation\n" +
			"is a server-side generation, so it is refused in read-only mode; pass\n" +
			"--latest to instead report the alert's most recent investigation (read-only,\n" +
			"no generation). --json prints the full investigation record, including the\n" +
			"embedded investigation steps and the notebook reference.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alertID := strings.TrimSpace(args[0])
			if alertID == "" {
				return fmt.Errorf("alert id is required")
			}
			if !latest {
				if err := refuseAIGenerationIfReadOnly(fmt.Sprintf("alert %s AI investigation", alertID)); err != nil {
					return err
				}
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			var inv *chronicle.Investigation
			if latest {
				// The UI's own filter grammar for "the investigation of this alert".
				filter := "alert_id='" + alertID + "' AND latest_in_alert=true"
				invs, err := c.ListInvestigationsFiltered(ctx, 100, filter, "start_time desc")
				if err != nil {
					return err
				}
				if len(invs) == 0 {
					return fmt.Errorf("no investigation exists for alert %s — run without --latest to start one", alertID)
				}
				inv = &invs[0]
			} else {
				if inv, err = c.TriggerInvestigation(ctx, alertID); err != nil {
					return err
				}
				if inv.InvestigationID() == "" {
					return fmt.Errorf("trigger returned no investigation id")
				}
			}

			poll := func() error {
				res, err := c.GetInvestigation(ctx, inv.InvestigationID())
				if err != nil {
					return err
				}
				prev := inv.Status
				inv = res
				if inv.Status != prev {
					fmt.Fprintf(os.Stderr, "  status: %s\n", inv.Status)
				}
				return nil
			}
			if err := aiPoll(poll, func() bool { return inv.Completed() }); err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, inv.Raw)
			}
			fmt.Fprintf(os.Stdout, "Investigation %s (alert %s)\n", inv.InvestigationID(), alertID)
			fmt.Fprintf(os.Stdout, "Status:     %s\n", inv.Status)
			if strings.HasSuffix(inv.Status, "_FAILURE") || strings.HasSuffix(inv.Status, "_ERROR") {
				fmt.Fprintf(os.Stderr, "investigation completed with error status %s\n", inv.Status)
				if inv.Summary != "" {
					fmt.Fprintf(os.Stderr, "  detail: %s\n", strings.TrimSpace(inv.Summary))
				}
				return fmt.Errorf("investigation failed: %s", inv.Status)
			}
			fmt.Fprintf(os.Stdout, "Verdict:    %s\n", orDash(inv.Verdict))
			fmt.Fprintf(os.Stdout, "Confidence: %s\n", orDash(inv.Confidence))
			if inv.Summary != "" {
				fmt.Fprintf(os.Stdout, "\n%s\n", strings.TrimSpace(inv.Summary))
			}
			if len(inv.NextSteps) > 0 {
				fmt.Fprintln(os.Stdout, "\nNext steps:")
				for _, s := range inv.NextSteps {
					fmt.Fprintf(os.Stdout, "  - [%s] %s\n", orDash(s.Type), s.Title)
				}
			}
			if id := inv.NotebookID(); id != "" {
				fmt.Fprintf(os.Stdout, "\nNotebook %s holds the agent's working detail (UDM queries per step) — use --json for the full record.\n", id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false, "read the alert's most recent investigation instead of starting a new one (read-only)")
	return markJSON(cmd)
}
