package cli

// soar_job_revision.go — job definition revision management (v1alpha).

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSOARJobRevisionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revision",
		Short: "Manage job definition revisions",
	}
	cmd.AddCommand(
		newSOARJobRevisionListCmd(),
		newSOARJobRevisionCreateCmd(),
		newSOARJobRevisionRollbackCmd(),
		newSOARJobRevisionDeleteCmd(),
	)
	return cmd
}

func newSOARJobRevisionListCmd() *cobra.Command {
	var (
		integration string
		job         string
	)
	cmd := &cobra.Command{
		Use:   "list --integration I --job J",
		Short: "List revisions of a job definition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := newSOARClient()
			if err != nil {
				return err
			}
			revisions, err := sc.ListJobRevisions(baseContext(), integration, job)
			if err != nil {
				return err
			}
			if jsonOut {
				raws := make([]json.RawMessage, len(revisions))
				for i, r := range revisions {
					raws[i] = r.Raw
				}
				return emitJSON(raws)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "NAME\tAUTHOR\tCOMMENT\tCREATE_TIME")
			for _, r := range revisions {
				ct := epochNumberToString(r.CreateTime)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					r.Name, defaultString(r.Author, "-"),
					defaultString(r.Comment, "-"), ct)
			}
			fmt.Fprintf(w, "\n%d revision(s)\n", len(revisions))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration identifier (required)")
	f.StringVar(&job, "job", "", "job id or name (required)")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("job")
	return markJSON(cmd)
}

func newSOARJobRevisionCreateCmd() *cobra.Command {
	var (
		integration string
		job         string
		comment     string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create --integration I --job J --comment '...'",
		Short: "MUTATING (guarded): snapshot the current job definition as a revision",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			// Fetch the current job definition to include in the snapshot.
			def, err := sc.GetJobDef(ctx, integration, job)
			if err != nil {
				return fmt.Errorf("get job definition: %w", err)
			}

			body := map[string]any{
				"job":     def.Raw,
				"comment": comment,
			}

			action := fmt.Sprintf("create revision for %s/%s", integration, job)
			dr, ay := soarGuard(action, dryRun, yes)
			if !jsonOut {
				fmt.Fprintf(os.Stdout, "Creating revision for job %s (integration %s)\n", job, integration)
				fmt.Fprintf(os.Stdout, "Comment: %s\n", defaultString(comment, "(none)"))
			}
			if dr {
				if !jsonOut {
					fmt.Fprintln(os.Stdout, "\nDRY RUN — no mutation sent. Re-run with --yes to apply.")
				}
				return emitGuardedResult(action, dr, false)
			}
			if !ay {
				if !jsonOut {
					fmt.Fprintln(os.Stdout, "\nRefusing to create without confirmation (pass --yes). Aborted.")
				}
				return emitGuardedResult(action, dr, false)
			}

			rev, err := sc.CreateJobRevision(ctx, integration, job, body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, rev.Raw)
			}
			fmt.Fprintf(os.Stdout, "Done. Revision created: %s\n", rev.Name)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration identifier (required)")
	f.StringVar(&job, "job", "", "job id or name (required)")
	f.StringVar(&comment, "comment", "", "revision comment")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("job")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}

func newSOARJobRevisionRollbackCmd() *cobra.Command {
	var (
		integration string
		job         string
		revision    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "rollback --integration I --job J --revision R",
		Short: "MUTATING (guarded): restore a job definition to a previous revision",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := newSOARClient()
			if err != nil {
				return err
			}

			action := fmt.Sprintf("rollback job %s/%s to revision %s", integration, job, revision)
			dr, ay := soarGuard(action, dryRun, yes)
			if !jsonOut {
				fmt.Fprintf(os.Stdout, "Rolling back job %s (integration %s) to revision %s\n", job, integration, revision)
			}
			if dr {
				if !jsonOut {
					fmt.Fprintln(os.Stdout, "\nDRY RUN — no mutation sent. Re-run with --yes to apply.")
				}
				return emitGuardedResult(action, dr, false)
			}
			if !ay {
				if !jsonOut {
					fmt.Fprintln(os.Stdout, "\nRefusing to rollback without confirmation (pass --yes). Aborted.")
				}
				return emitGuardedResult(action, dr, false)
			}

			resp, err := sc.RollbackJobRevision(baseContext(), integration, job, revision)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, resp)
			}
			fmt.Fprintln(os.Stdout, "Done. Job definition rolled back.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration identifier (required)")
	f.StringVar(&job, "job", "", "job id or name (required)")
	f.StringVar(&revision, "revision", "", "revision id to roll back to (required)")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("job")
	_ = cmd.MarkFlagRequired("revision")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}

func newSOARJobRevisionDeleteCmd() *cobra.Command {
	var (
		integration string
		job         string
		revision    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration I --job J --revision R",
		Short: "MUTATING (guarded): delete a job definition revision",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			action := fmt.Sprintf("delete revision %s of job %s/%s", revision, integration, job)
			return soarGuardedMutation(action, dryRun, yes, func() error {
				sc, err := newSOARClient()
				if err != nil {
					return err
				}
				return sc.DeleteJobRevision(baseContext(), integration, job, revision)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration identifier (required)")
	f.StringVar(&job, "job", "", "job id or name (required)")
	f.StringVar(&revision, "revision", "", "revision id to delete (required)")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("job")
	_ = cmd.MarkFlagRequired("revision")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}
