package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// Lane 3 — imperative SOAR case actions. These are per-case verbs (no
// desired-state file), so they are a command tree, not a reconcile surface. Each
// request body is modeled on the Siemplify external-API schema
// (third_party/siemplify-swagger.json): a numeric caseId, an optional
// alertIdentifier, and the verb's payload. Every verb is a LIVE mutation, so it
// shares the dry-run / --yes / banner guard with `soar push`.

func newSOARCaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "case <verb>",
		Short: "MUTATING (guarded): per-case actions (assign, tag, close, ...). Dry-run by default",
		Long: "Imperative case actions against the live SOAR tenant. caseId is the SOAR\n" +
			"integer id (not the SIEM UUID). Every verb defaults to a dry run; pass\n" +
			"--yes to apply (or confirm interactively).",
	}
	cmd.AddCommand(
		newCaseAssignCmd(),
		newCaseRenameCmd(),
		newCaseStageCmd(),
		newCaseTagCmd(false),
		newCaseTagCmd(true),
		newCaseDescribeCmd(),
		newCaseImportanceCmd(),
		newCaseCloseCmd(),
		newCaseMergeCmd(),
	)
	return cmd
}

// caseBody seeds a request body with the case id and (when set) the alert
// identifier — the two fields nearly every case verb shares.
func caseBody(caseID int, alert string) map[string]any {
	body := map[string]any{"caseId": caseID}
	if alert != "" {
		body["alertIdentifier"] = alert
	}
	return body
}

// caseAction is the shared guarded executor: it previews the body under a LIVE
// banner, stops on a dry run, refuses without confirmation, then calls do.
func caseAction(action string, body map[string]any, dryRun, yes bool, do func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error)) error {
	dr, ay := soarGuard(action, dryRun, yes)

	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SOAR case action against a PRODUCTION tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	pretty, _ := json.MarshalIndent(body, "", "  ")
	fmt.Fprintf(w, "Request body:\n%s\n\n", pretty)

	if dr {
		fmt.Fprintln(w, "DRY RUN -- no API call made. Re-run without --dry-run to apply.")
		return nil
	}
	if !ay {
		fmt.Fprintln(w, "Refusing to act without confirmation (pass --yes). Aborted.")
		return nil
	}
	// Build the client only when actually applying — dry runs need no credentials.
	lc, err := newSOARLegacyClient()
	if err != nil {
		return err
	}
	if _, err := do(baseContext(), lc); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done: %s.\n", action)
	return nil
}

// caseGuardFlags wires the shared --id/--alert/--dry-run/--yes flags onto a verb.
func caseGuardFlags(cmd *cobra.Command, caseID *int, alert *string, dryRun, yes *bool, withAlert bool) {
	f := cmd.Flags()
	f.IntVar(caseID, "id", 0, "SOAR case id (required)")
	if withAlert {
		f.StringVar(alert, "alert", "", "optional alert identifier to scope the action")
	}
	f.BoolVar(dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("id")
}

func newCaseAssignCmd() *cobra.Command {
	var (
		caseID      int
		alert, user string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "assign --id N --user <userId>",
		Short: "Assign a case to a user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := caseBody(caseID, alert)
			body["userId"] = user
			return caseAction(fmt.Sprintf("assign case %d -> %q", caseID, user), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.AssignUserToCase(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().StringVar(&user, "user", "", "target user id (required)")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func newCaseRenameCmd() *cobra.Command {
	var (
		caseID      int
		title       string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "rename --id N --title <s>",
		Short: "Rename a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{"caseId": caseID, "title": title}
			return caseAction(fmt.Sprintf("rename case %d", caseID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.RenameCase(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, nil, &dryRun, &yes, false)
	cmd.Flags().StringVar(&title, "title", "", "new case title (required)")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newCaseStageCmd() *cobra.Command {
	var (
		caseID       int
		alert, stage string
		dryRun, yes  bool
	)
	cmd := &cobra.Command{
		Use:   "stage --id N --stage <s>",
		Short: "Change a case's stage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := caseBody(caseID, alert)
			body["stage"] = stage
			return caseAction(fmt.Sprintf("set case %d stage -> %q", caseID, stage), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ChangeCaseStage(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().StringVar(&stage, "stage", "", "target stage (required)")
	_ = cmd.MarkFlagRequired("stage")
	return cmd
}

// newCaseTagCmd builds both `case tag` (add) and `case untag` (remove).
func newCaseTagCmd(remove bool) *cobra.Command {
	var (
		caseID      int
		alert, tag  string
		dryRun, yes bool
	)
	use, verb := "tag", "add tag to"
	if remove {
		use, verb = "untag", "remove tag from"
	}
	cmd := &cobra.Command{
		Use:   use + " --id N --tag <s>",
		Short: strings.ToUpper(use[:1]) + use[1:] + " a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := caseBody(caseID, alert)
			body["tag"] = tag
			return caseAction(fmt.Sprintf("%s case %d: %q", verb, caseID, tag), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					if remove {
						return lc.RemoveCaseTag(ctx, body)
					}
					return lc.AddCaseTag(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().StringVar(&tag, "tag", "", "tag value (required)")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}

func newCaseDescribeCmd() *cobra.Command {
	var (
		caseID      int
		description string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "describe --id N --description <s>",
		Short: "Change a case's description",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{"caseId": caseID, "description": description}
			return caseAction(fmt.Sprintf("set case %d description", caseID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ChangeCaseDescription(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, nil, &dryRun, &yes, false)
	cmd.Flags().StringVar(&description, "description", "", "new description (required)")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

func newCaseImportanceCmd() *cobra.Command {
	var (
		caseID      int
		alert       string
		important   bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "importance --id N --important",
		Short: "Mark a case important (or not)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := caseBody(caseID, alert)
			body["isImportant"] = important
			return caseAction(fmt.Sprintf("set case %d important=%v", caseID, important), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ChangeCaseImportanceStatus(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().BoolVar(&important, "important", true, "important flag value (default true; --important=false to clear)")
	return cmd
}

func newCaseCloseCmd() *cobra.Command {
	var (
		caseID             int
		alert, reason      string
		rootCause, comment string
		dryRun, yes        bool
	)
	cmd := &cobra.Command{
		Use:   "close --id N --reason <s>",
		Short: "Close a single case (string reason; see `soar push bulk-close` for queue bulk-close)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := caseBody(caseID, alert)
			body["reason"] = reason
			body["rootCause"] = rootCause
			body["comment"] = comment
			return caseAction(fmt.Sprintf("close case %d (reason=%q)", caseID, reason), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.CloseCase(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	f := cmd.Flags()
	f.StringVar(&reason, "reason", "", "close reason (required)")
	f.StringVar(&rootCause, "root-cause", "", "close root cause")
	f.StringVar(&comment, "comment", "", "close comment")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newCaseMergeCmd() *cobra.Command {
	var (
		idsArg      string
		into        int
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "merge --ids 1,2,3 --into N",
		Short: "Merge cases into a target case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids, err := parseIntList(idsArg)
			if err != nil {
				return err
			}
			body := map[string]any{"casesIds": ids, "caseToMergeWith": into}
			return caseAction(fmt.Sprintf("merge %v -> case %d", ids, into), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.MergeCases(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.StringVar(&idsArg, "ids", "", "comma-separated source case ids (required)")
	f.IntVar(&into, "into", 0, "target case id to merge into (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("ids")
	_ = cmd.MarkFlagRequired("into")
	return cmd
}
