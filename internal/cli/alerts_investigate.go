package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// `alerts investigate` — the per-alert AI (TIN) investigation flow. Default
// behavior mirrors the web UI: check for an existing completed investigation
// first and show it; only trigger a new one when none exists or --rerun is
// passed. --latest is the strict read-only variant (never triggers).

func newAlertsInvestigateCmd() *cobra.Command {
	var (
		latest bool
		rerun  bool
	)
	cmd := &cobra.Command{
		Use:   "investigate <alert-id>",
		Short: "AI investigation for one alert — shows existing result, or triggers a new one",
		Long: "Run (or view) the AI (Gemini) investigation for one detection alert.\n\n" +
			"Default: check for an existing completed investigation first and show it\n" +
			"(matching the web UI's behavior). If none exists, trigger a new one and poll\n" +
			"until it completes — typically a minute or two.\n\n" +
			"  --rerun   force a new investigation even if one already exists\n" +
			"  --latest  read-only: show the most recent investigation only (never trigger)\n\n" +
			"--json prints the full investigation record, including the embedded\n" +
			"investigation steps and the notebook reference.",
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

			switch {
			case latest:
				inv, err = latestInvestigation(c, alertID)
				if err != nil {
					return err
				}
				if inv == nil {
					return fmt.Errorf("no investigation exists for alert %s — run without --latest to start one", alertID)
				}
			case rerun:
				inv, err = triggerInvestigation(c, alertID)
				if err != nil {
					return err
				}
			default:
				inv, err = latestInvestigation(c, alertID)
				if err != nil {
					return err
				}
				if inv != nil && inv.Completed() {
					fmt.Fprintf(os.Stderr, "showing existing investigation (use --rerun to start a new one)\n")
				} else {
					fmt.Fprintf(os.Stderr, "no completed investigation found — triggering a new one\n")
					inv, err = triggerInvestigation(c, alertID)
					if err != nil {
						return err
					}
				}
			}

			if !inv.Completed() {
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
			}

			return printInvestigation(inv, alertID)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&latest, "latest", false, "read-only: show the most recent investigation (never trigger)")
	f.BoolVar(&rerun, "rerun", false, "force a new investigation even if one already exists")
	cmd.MarkFlagsMutuallyExclusive("latest", "rerun")
	return markJSON(cmd)
}

func latestInvestigation(c *chronicle.Client, alertID string) (*chronicle.Investigation, error) {
	filter := "alert_id='" + alertID + "' AND latest_in_alert=true"
	invs, err := c.ListInvestigationsFiltered(baseContext(), 100, filter, "start_time desc")
	if err != nil {
		return nil, err
	}
	if len(invs) == 0 {
		return nil, nil
	}
	return &invs[0], nil
}

func triggerInvestigation(c *chronicle.Client, alertID string) (*chronicle.Investigation, error) {
	inv, err := c.TriggerInvestigation(baseContext(), alertID)
	if err != nil {
		return nil, err
	}
	if inv.InvestigationID() == "" {
		return nil, fmt.Errorf("trigger returned no investigation id")
	}
	return inv, nil
}

func printInvestigation(inv *chronicle.Investigation, alertID string) error {
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
}
