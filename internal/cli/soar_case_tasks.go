package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// Case tasks — the checklist steps attached to a case (assign/track containment
// work). list is read-only; add/done/delete are guarded LIVE mutations on the
// legacy external API. Bodies are modeled on the Siemplify task schema.
func newCaseTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task <verb>",
		Short: "Case checklist tasks: list (read) + guarded add/done/delete",
	}
	cmd.AddCommand(newCaseTaskListCmd(), newCaseTaskAddCmd(), newCaseTaskDoneCmd(), newCaseTaskDeleteCmd())
	return cmd
}

func newCaseTaskListCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "list --id N",
		Short: "Read-only: list the tasks on a case",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := c.CaseXListTasksByRequest(baseContext(), url.Values{"caseId": {fmt.Sprintf("%d", caseID)}})
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().IntVar(&caseID, "id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("id")
	return markJSON(cmd)
}

func newCaseTaskAddCmd() *cobra.Command {
	var (
		caseID         int
		title, content string
		dryRun, yes    bool
	)
	cmd := &cobra.Command{
		Use:   "add --id N --title <s> [--content <s>]",
		Short: "Add a checklist task to a case (guarded)",
		Long: "Create a task on a case — a tracked containment/triage step. --id is the SOAR\n" +
			"integer case id. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			body := map[string]any{"caseId": caseID, "title": title, "content": content}
			return caseAction(fmt.Sprintf("add task to case %d: %q", caseID, title), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.CreateCaseTask(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required)")
	f.StringVar(&title, "title", "", "task title (required)")
	f.StringVar(&content, "content", "", "task content/description")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("title")
	return markJSON(cmd)
}

func newCaseTaskDoneCmd() *cobra.Command {
	var (
		taskID      int
		comment     string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "done --task-id N [--comment <s>]",
		Short: "Mark a case task done (guarded)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			body := map[string]any{"id": taskID, "completionComment": comment}
			return caseAction(fmt.Sprintf("mark task %d done", taskID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.MarkCaseTaskDone(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&taskID, "task-id", 0, "task id (required) — from `cases task list`")
	f.StringVar(&comment, "comment", "", "completion comment")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("task-id")
	return markJSON(cmd)
}

func newCaseTaskDeleteCmd() *cobra.Command {
	var (
		taskID      int
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "delete --task-id N",
		Short: "Delete a case task (guarded)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			id := fmt.Sprintf("%d", taskID)
			return caseAction(fmt.Sprintf("delete task %d", taskID), map[string]any{"id": taskID}, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.DeleteCaseTask(ctx, id)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&taskID, "task-id", 0, "task id (required) — from `cases task list`")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("task-id")
	return markJSON(cmd)
}
