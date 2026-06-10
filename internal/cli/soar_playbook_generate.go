package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// `soar playbook generate` (Wave 56) — Gemini playbook drafting. Generation
// CREATES a playbook draft on the live tenant, so it rides the standard guard;
// the result then flows through the existing review loop (`soar playbook
// validate` → `soar push playbook --dry-run` → guarded save).

func newSOARPlaybookGenerateCmd() *cobra.Command {
	var (
		description string
		caseID      int
		alert       string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "generate (--description <text> | --case-id N --alert <id>)",
		Short: "MUTATING (guarded): generate a playbook draft with AI",
		Long: "Draft a playbook with Gemini — from a free-text description, or from a\n" +
			"specific alert in a case (\"build a playbook for this alert pattern\").\n" +
			"Generation creates a DRAFT playbook on the tenant and may run\n" +
			"asynchronously — poll the by-alert form with `generate-status`. Review the\n" +
			"draft with `soar playbook validate` and the standard guarded save loop\n" +
			"before enabling.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			byAlert := caseID != 0 || alert != ""
			if byAlert && (caseID == 0 || alert == "") {
				return fmt.Errorf("--case-id and --alert go together")
			}
			if !byAlert && description == "" {
				return fmt.Errorf("pass --description, or --case-id with --alert")
			}
			var (
				action string
				body   map[string]any
			)
			if byAlert {
				action = fmt.Sprintf("generate playbook draft from alert %s (case %d)", alert, caseID)
				body = map[string]any{
					"caseId":         strconv.Itoa(caseID),
					"alertId":        alert,
					"isFirstRequest": true,
				}
			} else {
				action = "generate playbook draft from description"
				body = map[string]any{"description": description}
			}
			return caseAction(action, body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					if byAlert {
						return lc.AiGeneratePlaybookByAlert(ctx, body)
					}
					return lc.AiGeneratePlaybook(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.StringVar(&description, "description", "", "free-text description of the playbook to draft")
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (with --alert: draft from that alert)")
	f.StringVar(&alert, "alert", "", "alert id within the case")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
}

// newSOARPlaybookGenerateStatusCmd polls the by-alert AI generation status —
// the read companion of `generate --case-id … --alert …` (the generation runs
// asynchronously server-side).
func newSOARPlaybookGenerateStatusCmd() *cobra.Command {
	var (
		caseID int
		alert  string
	)
	cmd := &cobra.Command{
		Use:   "generate-status --case-id N --alert <id>",
		Short: "Read-only: poll the status of a by-alert AI playbook generation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.AiGenerationStatusByAlert(baseContext(), map[string]any{
				"caseId":  strconv.Itoa(caseID),
				"alertId": alert,
			})
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&alert, "alert", "", "alert id within the case (required)")
	_ = cmd.MarkFlagRequired("case-id")
	_ = cmd.MarkFlagRequired("alert")
	return cmd
}
