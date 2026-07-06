package cli

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

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

func newCaseAssignCmd() *cobra.Command {
	var (
		caseID      int
		alert, user string
		idsArg      string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "assign (--id N | --ids 1,2,3) --user <userId>",
		Short: "Assign a case (or many) to a user",
		Long: "Assign one or more cases to an analyst or role. --id is the SOAR integer case\n" +
			"id from `soar case list`; --ids assigns a whole set in one bulk call\n" +
			"(ExecuteBulkAssign). Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if idsArg != "" {
				ids, err := parseIntList(idsArg)
				if err != nil {
					return err
				}
				body := map[string]any{"casesIds": ids, "userName": user}
				return caseAction(fmt.Sprintf("assign case(s) %v -> %q", ids, user), body, dryRun, yes,
					func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
						return lc.BulkAssign(ctx, body)
					})
			}
			if caseID == 0 {
				return fmt.Errorf("a case id is required (--id or --ids)")
			}
			body := caseBody(caseID, alert)
			body["userId"] = user
			return caseAction(fmt.Sprintf("assign case %d -> %q", caseID, user), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.AssignUserToCase(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id")
	f.StringVar(&idsArg, "ids", "", "comma-separated case ids (bulk form)")
	f.StringVar(&alert, "alert", "", "scope a single-case assign to one alert (ignored with --ids)")
	guardRunFlags(cmd, &dryRun, &yes)
	cmd.MarkFlagsMutuallyExclusive("id", "ids")
	f.StringVar(&user, "user", "", "target user — a username (list them with 'soar users list') or a role as @RoleName (required)")
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
		Short: "Rename a case's title",
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
		idsArg       string
		dryRun, yes  bool
	)
	cmd := &cobra.Command{
		Use:   "stage (--id N | --ids 1,2,3) --stage <s>",
		Short: "Change a case's (or many cases') stage",
		Long: "Move one or more cases to a workflow stage (list valid stages with\n" +
			"`soar case values stages`). --id is the SOAR integer case id from\n" +
			"`soar case list`; --ids moves a whole set in one bulk call\n" +
			"(ExecuteBulkChangeCaseStage). Guarded: dry-run by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if idsArg != "" {
				ids, err := parseIntList(idsArg)
				if err != nil {
					return err
				}
				body := map[string]any{"casesIds": ids, "stage": stage}
				return caseAction(fmt.Sprintf("set case(s) %v stage -> %q", ids, stage), body, dryRun, yes,
					func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
						return lc.BulkChangeCaseStage(ctx, body)
					})
			}
			if caseID == 0 {
				return fmt.Errorf("a case id is required (--id or --ids)")
			}
			body := caseBody(caseID, alert)
			body["stage"] = stage
			return caseAction(fmt.Sprintf("set case %d stage -> %q", caseID, stage), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ChangeCaseStage(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id")
	f.StringVar(&idsArg, "ids", "", "comma-separated case ids (bulk form)")
	f.StringVar(&alert, "alert", "", "scope a single-case stage change to one alert (ignored with --ids)")
	guardRunFlags(cmd, &dryRun, &yes)
	cmd.MarkFlagsMutuallyExclusive("id", "ids")
	f.StringVar(&stage, "stage", "", "target stage (required) — list valid stages with 'soar case values stages'")
	_ = cmd.MarkFlagRequired("stage")
	return markJSON(cmd)
}

// newCaseTagCmd builds both `case tag` (add) and `case untag` (remove). The add
// form takes --ids for a bulk tag (ExecuteBulkAddCaseTag); untag stays single
// (there is no bulk-remove-tag endpoint).
func newCaseTagCmd(remove bool) *cobra.Command {
	var (
		caseID      int
		alert, tag  string
		idsArg      string
		dryRun, yes bool
	)
	use, verb := "tag", "add tag to"
	short := "Add a tag to a case (supports bulk --ids)"
	if remove {
		use, verb = "untag", "remove tag from"
		short = "Remove a tag from a case"
	}
	useLine := use + " --id N --tag <s>"
	if !remove {
		useLine = use + " (--id N | --ids 1,2,3) --tag <s>"
	}
	cmd := &cobra.Command{
		Use:   useLine,
		Short: short,
		Long: strings.ToUpper(verb[:1]) + verb[1:] + " a case (list existing tags with\n" +
			"`soar case values tags`). --id is the SOAR integer case id from\n" +
			"`soar case list`. Guarded: dry-run by default, --yes to apply live.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !remove && idsArg != "" {
				ids, err := parseIntList(idsArg)
				if err != nil {
					return err
				}
				body := map[string]any{"casesIds": ids, "tags": []string{tag}}
				return caseAction(fmt.Sprintf("add tag to case(s) %v: %q", ids, tag), body, dryRun, yes,
					func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
						return lc.BulkAddCaseTag(ctx, body)
					})
			}
			if caseID == 0 {
				return fmt.Errorf("a case id is required (--id%s)", map[bool]string{true: "", false: " or --ids"}[remove])
			}
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
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id")
	f.StringVar(&alert, "alert", "", "scope a single-case tag to one alert (ignored with --ids)")
	if !remove {
		f.StringVar(&idsArg, "ids", "", "comma-separated case ids (bulk add form)")
		cmd.MarkFlagsMutuallyExclusive("id", "ids")
	}
	guardRunFlags(cmd, &dryRun, &yes)
	f.StringVar(&tag, "tag", "", "tag value (required) — list existing tags with 'soar case values tags'")
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

func newCaseIncidentCmd() *cobra.Command {
	var (
		caseID      int
		unset       bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "incident --id N",
		Short: "Mark (or unmark) a case as an incident",
		Long: "Set the incident flag on a case. Pass --unset to remove the incident flag.\n" +
			"The case must be assigned to the current user. Guarded: dry-run by default,\n" +
			"--yes to apply live.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{"caseId": caseID}
			verb := "mark"
			if unset {
				verb = "unmark"
			}
			return caseAction(fmt.Sprintf("%s case %d as incident", verb, caseID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					if unset {
						return lc.DynamicCaseXUnraiseIncident(ctx, body)
					}
					return lc.DynamicCaseXRaiseIncident(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, nil, &dryRun, &yes, false)
	cmd.Flags().BoolVar(&unset, "unset", false, "remove the incident flag instead of setting it")
	return markJSON(cmd)
}

func newCaseReportCmd() *cobra.Command {
	var (
		caseID  int
		format  string
		outFile string
	)
	cmd := &cobra.Command{
		Use:   "report --id N [--format pdf|doc|xlsx|csv] [--out FILE]",
		Short: "Generate and download a case report",
		Long: "Generate a case report in the specified format. The response is binary (the\n" +
			"report file itself). Use --out to save to a file; without it, writes to stdout\n" +
			"(pipe to a file). Formats: pdf (default), doc, docx, xlsx, csv, html.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--id is required")
			}
			kindMap := map[string]int{
				"pdf": 0, "rtf": 1, "doc": 2, "docx": 3,
				"html": 4, "xlsx": 5, "csv": 6,
			}
			kind, ok := kindMap[strings.ToLower(format)]
			if !ok {
				return fmt.Errorf("unknown format %q — use pdf, doc, docx, xlsx, csv, or html", format)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			body := map[string]any{"caseId": caseID, "reportKind": kind}
			data, err := lc.DynamicCaseXGenerateReportBytes(baseContext(), body)
			if err != nil {
				return err
			}
			if outFile != "" {
				if err := os.WriteFile(outFile, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "report saved to %s (%d bytes)\n", outFile, len(data))
				return nil
			}
			_, err = os.Stdout.Write(data)
			return err
		},
	}
	cmd.Flags().IntVar(&caseID, "id", 0, "SOAR case id (required)")
	cmd.Flags().StringVar(&format, "format", "pdf", "report format: pdf, doc, docx, xlsx, csv, html")
	cmd.Flags().StringVar(&outFile, "out", "", "save report to this file path")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
