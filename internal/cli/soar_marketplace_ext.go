package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newSOARFeaturedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "featured <verb>",
		Short: "Browse and install Google-curated featured playbooks",
	}
	cmd.AddCommand(newSOARFeaturedListCmd(), newSOARFeaturedInstallCmd())
	return cmd
}

func newSOARFeaturedListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List featured playbooks from the Content Hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListFeaturedPlaybooks(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "featured playbooks", raw)
			return nil
		},
	}
	return markJSON(cmd)
}

func newSOARFeaturedInstallCmd() *cobra.Command {
	var (
		name   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "install --name <resource-name>",
		Short: "MUTATING (guarded): install a featured playbook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required (the resource name from `featured list --json`)")
			}
			label := "featured install " + lastSegment(name)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Install featured playbook: %s\n", name)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refused. Pass --yes.")
				return nil
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			_, err = mc.InstallFeaturedPlaybook(baseContext(), name, nil)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "installed.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "featured playbook resource name (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func newSOARMarketplaceDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <integration-id>",
		Short: "Show the diff between installed and marketplace version of an integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.FetchCommercialDiff(baseContext(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "commercial diff", raw)
			return nil
		},
	}
	return markJSON(cmd)
}
