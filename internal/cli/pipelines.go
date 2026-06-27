package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline <verb>",
		Short: "Manage log processing pipelines (list / get / delete)",
	}
	cmd.AddCommand(
		newPipelineListCmd(),
		newPipelineGetCmd(),
		newPipelineDeleteCmd(),
	)
	return cmd
}

func newPipelineListCmd() *cobra.Command {
	var filter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List log processing pipelines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ps, err := c.ListPipelines(baseContext(), filter)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(ps)
			}
			if len(ps) == 0 {
				fmt.Fprintln(os.Stdout, "no pipelines.")
				return nil
			}
			for _, p := range ps {
				fmt.Fprintf(os.Stdout, "%-10s %-40s %s\n", orDash(p.Description), lastSegment(p.Name), orDash(p.DisplayName))
			}
			fmt.Fprintf(os.Stdout, "\n%d pipeline(s).\n", len(ps))
			return nil
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "AIP-160 filter expression")
	return markJSON(cmd)
}

func newPipelineGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one pipeline by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			p, err := c.GetPipeline(baseContext(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(p)
			}
			fmt.Fprintf(os.Stdout, "name:    %s\nstate:   %s\ndisplay: %s\n", p.Name, orDash(p.Description), orDash(p.DisplayName))
			return nil
		},
	}
	return markJSON(cmd)
}

func newPipelineDeleteCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "MUTATING (guarded): delete a log processing pipeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			action := "pipeline delete " + id
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				return c.DeletePipeline(baseContext(), id, "")
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
