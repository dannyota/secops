package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Integration lifecycle verbs: `soar integration install` / `uninstall`
// (Content Hub package install, guarded removal).

func newSOARIntegrationInstallCmd() *cobra.Command {
	var (
		identifier  string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "install --identifier <marketplace-id>",
		Short: "MUTATING (guarded): install a Content Hub marketplace integration",
		Long: "Install a marketplace integration pack by its identifier (from\n" +
			"`soar marketplace list`). Guarded: dry-run by default, --yes to apply.\n" +
			"Configure an instance afterwards with `integrations create`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := fmt.Sprintf("integration install %s", identifier)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would install marketplace integration %q. Re-run with --yes.\n", identifier)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to install without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			out, err := c.InstallMarketplaceIntegration(baseContext(), identifier, map[string]any{})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(out)
			}
			fmt.Printf("Installed marketplace integration %q.\n", identifier)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&identifier, "identifier", "", "marketplace integration identifier (from 'soar marketplace list') (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("identifier")
	return markJSON(cmd)
}

// newSOARIntegrationConnectorCmd groups the connector-DEFINITION verbs (the
// connector templates inside an integration, as opposed to the configured
// connector instances under `soar pull/push connectors`).
func newSOARIntegrationUninstallCmd() *cobra.Command {
	var (
		key    string
		name   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall --key <integration-key>",
		Short: "Delete a custom integration pack (clone) by its key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --name is the deprecated alias of --key (the value is an integration
			// key, never a display name).
			if key == "" {
				key = name
			}
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("--key is required")
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			target, err := resolveCustomIntegration(ctx, c, key)
			if err != nil {
				return err
			}
			dr, _ := soarGuard("integration uninstall", dryRun, yes)
			key := target.Name
			if key == "" {
				key = target.Identifier
			}
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN — would delete custom integration %q (%s)\n", target.DisplayName, key)
				return nil
			}
			if err := c.DeleteIntegration(ctx, key); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted custom integration %q (%s)\n", target.DisplayName, key)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&key, "key", "", "integration key: Name (clone), Identifier, or displayName (required)")
	f.StringVar(&name, "name", "", "deprecated alias of --key")
	_ = f.MarkHidden("name")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

// resolveCustomIntegration finds the integration addressed by key (matched against
// Name, Identifier, or DisplayName) and refuses anything that isn't custom — the
// guardrail against deleting a commercial pack or the stock base integration.
