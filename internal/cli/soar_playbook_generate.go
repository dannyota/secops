package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	var name string
	cmd := &cobra.Command{
		Use:   "generate (--description <text> | --case-id N --alert <id>)",
		Short: "MUTATING (guarded): generate a playbook draft with AI",
		Long: "Draft a playbook with Gemini — from a free-text description, or from a\n" +
			"specific alert in a case (\"build a playbook for this alert pattern\").\n" +
			"The description form is synchronous and returns the generated DRAFT\n" +
			"definition without persisting anything — review it and save with\n" +
			"`soar push playbook --file`. The by-alert form may run asynchronously —\n" +
			"poll it with `generate-status`. The Playbook Assistant API may reject\n" +
			"API-key auth by server policy; the error says so plainly.",
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
				body   any
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
				envelope, err := legacy.NewAiGenerateRequest(description, name)
				if err != nil {
					return err
				}
				body = envelope
			}
			err := caseAction(action, body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					if byAlert {
						return lc.AiGeneratePlaybookByAlert(ctx, body)
					}
					return lc.AiGeneratePlaybook(ctx, body)
				})
			if err != nil && strings.Contains(err.Error(), "restricted for API keys") {
				return fmt.Errorf("the Playbook Assistant rejects API-key auth on this instance (server policy) — generate the draft in the web UI designer instead: %w", err)
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&description, "description", "", "free-text description of the playbook to draft")
	f.StringVar(&name, "name", "ai-draft", "working name for the description-form draft")
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
