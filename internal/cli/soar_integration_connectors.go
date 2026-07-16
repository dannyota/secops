package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// The `soar integration connector-def` sub-tree: list/delete connector
// definitions shipped by an integration.

func newSOARIntegrationConnectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connector",
		Short: "Manage connector definitions inside an integration",
	}
	cmd.AddCommand(newSOARConnectorDefListCmd(), newSOARConnectorDefDeleteCmd())
	return cmd
}

func newSOARConnectorDefListCmd() *cobra.Command {
	var integration string
	cmd := &cobra.Command{
		Use:   "list --integration <key>",
		Short: "List an integration's connector definitions (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			defs, err := c.ListConnectors(baseContext(), integration)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(defs)
			}
			for _, d := range defs {
				tag := ""
				if d.Custom {
					tag = "  [custom/deletable]"
				}
				fmt.Fprintf(os.Stdout, "%-6s %s%s\n", d.ID.String(), d.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d connector definition(s)\n", len(defs))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier (required)")
	_ = cmd.MarkFlagRequired("integration")
	return markJSON(cmd)
}

func newSOARConnectorDefDeleteCmd() *cobra.Command {
	var (
		integration string
		id          string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration <key> --id <connector-id>",
		Short: "Delete a custom connector definition (e.g. a 'Copy of …' duplicate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			def, err := c.GetConnectorDef(ctx, integration, id)
			if err != nil {
				return fmt.Errorf("connector definition %s/%s not found: %w", integration, id, err)
			}
			if !def.Custom {
				return fmt.Errorf("connector %q (id %s) is a commercial definition, not deletable", def.DisplayName, id)
			}
			action := fmt.Sprintf("delete custom connector definition %q (%s/%s)", def.DisplayName, integration, id)
			return soarGuardedMutation(action, dryRun, yes, func() error {
				if err := c.DeleteConnectorDef(ctx, integration, id); err != nil {
					return err
				}
				if !jsonOut {
					fmt.Fprintf(os.Stdout, "deleted custom connector definition %q (%s/%s)\n", def.DisplayName, integration, id)
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier (required)")
	f.StringVar(&id, "id", "", "numeric connector-definition id (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

// newSOARIntegrationUninstallCmd deletes a CUSTOM integration pack (e.g. a cloned
// "Copy of …") by its addressable key via the v1alpha integrations.delete path.
// Commercial/marketplace packs are not deletable. Guarded LIVE MUTATION.
