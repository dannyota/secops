package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
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
		Short: "Per-case triage: read (list, get) + guarded mutations (assign, tag, close, ...)",
		Long: "Per-case workflow against the live SOAR tenant (AppKey, the reliable lane).\n" +
			"caseId is the SOAR integer id (not the SIEM UUID). `list` and `get` read\n" +
			"only; every mutating verb defaults to a dry run — pass --yes to apply (or\n" +
			"confirm interactively).",
	}
	cmd.AddCommand(
		newCaseListCmd(),
		newCaseGetCmd(),
		newCaseRunActionCmd(),
		newCaseSimulationCmd(),
		newCaseChatCmd(),
		newCaseCustomFieldsCmd(),
		newCaseWallCmd(),
		newCaseContextCmd(),
		newCaseAssignCmd(),
		newCaseRenameCmd(),
		newCaseStageCmd(),
		newCaseTagCmd(false),
		newCaseTagCmd(true),
		newCaseDescribeCmd(),
		newCaseImportanceCmd(),
		newCasePriorityCmd(),
		newCaseCloseCmd(),
		newCaseReopenCmd(),
		newCaseMergeCmd(),
		newCaseCommentCmd(),
		newCaseAlertCmd(),
		newCaseValuesCmd(),
		newCaseSummarizeCmd(),
		newCaseCountsCmd(),
	)
	return cmd
}

// guardRunFlags wires the shared --dry-run/--yes apply gate onto a verb (the
// piece of caseGuardFlags that every guarded verb shares regardless of its
// id/alert flag shape).
func guardRunFlags(cmd *cobra.Command, dryRun, yes *bool) {
	f := cmd.Flags()
	f.BoolVar(dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
}

func newCasePriorityCmd() *cobra.Command {
	var (
		caseID          int
		alert, priority string
		dryRun, yes     bool
	)
	cmd := &cobra.Command{
		Use:   "priority --id N --priority <level>",
		Short: "Change a case's priority",
		Long: "Escalate or downgrade a case's priority (distinct from the `importance`\n" +
			"flag): informative | low | medium | high | critical.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := legacy.ParseCasePriority(priority)
			if err != nil {
				return err
			}
			body := caseBody(caseID, alert)
			body["priority"] = int(p)
			return caseAction(fmt.Sprintf("set case %d priority -> %s", caseID, p), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ChangeCasePriority(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().StringVar(&priority, "priority", "", "target priority: informative|low|medium|high|critical (required)")
	_ = cmd.MarkFlagRequired("priority")
	return markJSON(cmd)
}

func newCaseReopenCmd() *cobra.Command {
	var (
		caseID      int
		idsArg      string
		comment     string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "reopen (--id N | --ids 1,2,3) [--comment <s>]",
		Short: "Reopen closed case(s) — the inverse of close",
		Long: "Reopen one or more closed cases (ExecuteBulkReopenCase). The inverse of\n" +
			"`close` / `soar push bulk-close`, so a wrong close is recoverable in-tool.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var ids []int
			switch {
			case idsArg != "":
				parsed, err := parseIntList(idsArg)
				if err != nil {
					return err
				}
				ids = parsed
			case caseID != 0:
				ids = []int{caseID}
			default:
				return fmt.Errorf("a case id is required (--id or --ids)")
			}
			body := map[string]any{"casesIds": ids, "reopenComment": comment}
			return caseAction(fmt.Sprintf("reopen case(s) %v", ids), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.BulkReopenCase(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id")
	f.StringVar(&idsArg, "ids", "", "comma-separated case ids (bulk form)")
	f.StringVar(&comment, "comment", "", "reopen comment (free-text note)")
	guardRunFlags(cmd, &dryRun, &yes)
	cmd.MarkFlagsMutuallyExclusive("id", "ids")
	return markJSON(cmd)
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
// body is the request payload — a map or one of the typed soar/legacy request
// structs — rendered verbatim in the preview and the --json result.
func caseAction(action string, body any, dryRun, yes bool, do func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error)) error {
	dr, ay := soarGuard(action, dryRun, yes)

	// emit prints the machine-readable result under --json so the dry-run → --yes →
	// verify loop is scriptable. applied = the mutation actually ran.
	emit := func(applied bool) error {
		if !jsonOut {
			return nil
		}
		return emitJSON(struct {
			Action  string `json:"action"`
			Request any    `json:"request"`
			DryRun  bool   `json:"dry_run"`
			Applied bool   `json:"applied"`
			OK      bool   `json:"ok"`
		}{Action: action, Request: body, DryRun: dr, Applied: applied, OK: true})
	}

	if !jsonOut {
		w := os.Stdout
		bar := strings.Repeat("!", 72)
		fmt.Fprintln(w, bar)
		fmt.Fprintln(w, "!! LIVE SOAR case action against a PRODUCTION tenant !!")
		fmt.Fprintf(w, "!! Action: %s\n", action)
		fmt.Fprintln(w, bar)
		pretty, _ := json.MarshalIndent(body, "", "  ")
		fmt.Fprintf(w, "Request body:\n%s\n\n", pretty)
	}

	if dr {
		if !jsonOut {
			fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run without --dry-run to apply.")
		}
		return emit(false)
	}
	if !ay {
		if !jsonOut {
			fmt.Fprintln(os.Stdout, "Refusing to act without confirmation (pass --yes). Aborted.")
		}
		return emit(false)
	}
	// Build the client only when actually applying — dry runs need no credentials.
	lc, err := newSOARLegacyClient()
	if err != nil {
		return err
	}
	if _, err := do(baseContext(), lc); err != nil {
		return err
	}
	if !jsonOut {
		fmt.Fprintf(os.Stdout, "Done: %s.\n", action)
	}
	return emit(true)
}

// caseGuardFlags wires the shared --id/--alert/--dry-run/--yes flags onto a verb.
func caseGuardFlags(cmd *cobra.Command, caseID *int, alert *string, dryRun, yes *bool, withAlert bool) {
	f := cmd.Flags()
	f.IntVar(caseID, "id", 0, "SOAR case id (required)")
	if withAlert {
		f.StringVar(alert, "alert", "", "scope the action to one alert in the case instead of the whole case "+
			"(alert identifier from 'soar case get <id>'); omit to act on the case")
	}
	guardRunFlags(cmd, dryRun, yes)
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
		Long: "Assign a case to an analyst or role. --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
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
	cmd.Flags().StringVar(&user, "user", "", "target user id — a username (list them with 'soar users list') or a role as @RoleName (required)")
	_ = cmd.MarkFlagRequired("user")
	return markJSON(cmd)
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
		Long: "Change a case's title. --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
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
	return markJSON(cmd)
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
		Long: "Move a case to a different workflow stage (list valid stages with\n" +
			"`soar case values stages`). --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
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
	cmd.Flags().StringVar(&stage, "stage", "", "target stage (required) — list valid stages with 'soar case values stages'")
	_ = cmd.MarkFlagRequired("stage")
	return markJSON(cmd)
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
		Long: strings.ToUpper(verb[:1]) + verb[1:] + " a case (list existing tags with\n" +
			"`soar case values tags`). --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
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
	cmd.Flags().StringVar(&tag, "tag", "", "tag value (required) — list existing tags with 'soar case values tags'")
	_ = cmd.MarkFlagRequired("tag")
	return markJSON(cmd)
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
		Long: "Replace a case's description text. --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
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
	return markJSON(cmd)
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
		Long: "Set or clear a case's important flag (--important=false clears it). --id is\n" +
			"the SOAR integer case id from `soar case list`. Guarded: dry-run by default,\n" +
			"--yes to apply live.",
		Args: cobra.NoArgs,
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
	return markJSON(cmd)
}

func newCaseCloseCmd() *cobra.Command {
	var (
		caseID             int
		alert, reason      string
		rootCause, comment string
		dryRun, yes        bool
	)
	cmd := &cobra.Command{
		Use:   "close --id N --reason <enum>",
		Short: "Close a single case (typed reason enum, same vocabulary as `soar push bulk-close`)",
		Long: "Close one case. --reason is the fixed close-reason enum (the same set\n" +
			"`soar push bulk-close` uses) so single and bulk closes aggregate in metrics:\n" +
			"malicious | not-malicious | maintenance | inconclusive | unknown. Put your\n" +
			"custom root-cause name in --root-cause and any free-text note in --comment.\n" +
			"--id is the SOAR integer case id from `soar case list`. Guarded: dry-run by\n" +
			"default, --yes to apply live.",
		Example: "  # preview closing a false positive (dry run)\n" +
			"  secopsctl soar case close --id 1234 --reason not-malicious\n\n" +
			"  # apply for real with a note\n" +
			"  secopsctl soar case close --id 1234 --reason malicious \\\n" +
			"      --comment 'confirmed C2 beacon' --yes",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cr, err := parseCloseReason(reason)
			if err != nil {
				return err
			}
			body := caseBody(caseID, alert)
			// The legacy external CloseCase types `reason` as a free string, but the
			// Api* close family puts the PascalCase enum NAME on the wire (the sibling
			// ApiCloseAlertRequest examples reason="NotMalicious" in the swagger) — which
			// is exactly CloseReason.String(), so the typed token maps straight through.
			body["reason"] = cr.String()
			body["rootCause"] = rootCause
			body["comment"] = comment
			return caseAction(fmt.Sprintf("close case %d (reason=%s)", caseID, cr), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.CloseCase(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	f := cmd.Flags()
	f.StringVar(&reason, "reason", "", "close reason: malicious | not-malicious | maintenance | inconclusive | unknown (required)")
	f.StringVar(&rootCause, "root-cause", "", "close root cause (your custom root-cause name) — list options with 'soar case values root-causes'")
	f.StringVar(&comment, "comment", "", "close comment (free-text note)")
	_ = cmd.MarkFlagRequired("reason")
	return markJSON(cmd)
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
		Long: "Merge source cases into a target case; the target id is added to the set\n" +
			"automatically if omitted. All ids are SOAR integer case ids from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids, err := parseIntList(idsArg)
			if err != nil {
				return err
			}
			// The API rejects a merge whose target is not in the selected set
			// ("Cannot merge cases with case that is not selected"), so ensure the
			// target id is present in casesIds.
			if !slices.Contains(ids, into) {
				ids = append(ids, into)
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
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("ids")
	_ = cmd.MarkFlagRequired("into")
	return markJSON(cmd)
}
