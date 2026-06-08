package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/soar"
)

// newSOARIntegrationCmd groups the imperative integration-instance verbs.
// Integration instances are not reconcilable (no update endpoint, no round-tripping
// read shape), so they are operated imperatively; reads stay on `soar legacy call`.
func newSOARIntegrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Manage SOAR integration instances (imperative create/delete)",
	}
	cmd.AddCommand(newSOARIntegrationCreateCmd(), newSOARIntegrationDeleteCmd(),
		newSOARIntegrationListCmd(), newSOARIntegrationInstallCmd(), newSOARIntegrationUninstallCmd(),
		newSOARIntegrationConnectorCmd())
	return cmd
}

// newSOARIntegrationInstallCmd installs a Content Hub marketplace integration by
// identifier — the missing half of `uninstall`, closing the browse → install →
// create-instance loop. Guarded; live validation deferred.
func newSOARIntegrationInstallCmd() *cobra.Command {
	var (
		identifier  string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "install --identifier <marketplace-id>",
		Short: "Install a Content Hub marketplace integration (guarded)",
		Long: "Install a marketplace integration pack by its identifier (from\n" +
			"`soar marketplace list`). Guarded: dry-run by default, --yes to apply.\n" +
			"Configure an instance afterwards with `soar integration create`.",
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
	f.StringVar(&identifier, "identifier", "", "marketplace integration identifier (from `soar marketplace list`) (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("identifier")
	return cmd
}

// newSOARIntegrationConnectorCmd groups the connector-DEFINITION verbs (the
// connector templates inside an integration, as opposed to the configured
// connector instances under `soar pull/push connectors`).
func newSOARIntegrationConnectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connector",
		Short: "List/delete connector definitions inside an integration",
	}
	cmd.AddCommand(newSOARConnectorDefListCmd(), newSOARConnectorDefDeleteCmd())
	return cmd
}

func newSOARConnectorDefListCmd() *cobra.Command {
	var (
		integration string
		asJSON      bool
	)
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
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(defs)
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
	f.BoolVar(&asJSON, "json", false, "emit JSON")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
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
			dr, _ := soarGuard("integration connector delete", dryRun, yes)
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN: would delete custom connector definition %q (%s/%s)\n", def.DisplayName, integration, id)
				return nil
			}
			if err := c.DeleteConnectorDef(ctx, integration, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted custom connector definition %q (%s/%s)\n", def.DisplayName, integration, id)
			return nil
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

// newSOARIntegrationListCmd lists installed integration packs via the modern
// v1alpha surface — the discovery side of uninstall. Read-only.
func newSOARIntegrationListCmd() *cobra.Command {
	var (
		asJSON bool
		custom bool
	)
	cmd := &cobra.Command{
		Use:   "list [--custom] [--json]",
		Short: "List installed integration packs (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ints, err := c.ListIntegrations(baseContext())
			if err != nil {
				return err
			}
			if custom {
				ints = slices.DeleteFunc(ints, func(i soar.Integration) bool { return !soar.IsDeletableIntegration(i) })
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(ints)
			}
			for _, i := range ints {
				tag := ""
				if soar.IsDeletableIntegration(i) {
					tag = "  [deletable]"
				}
				fmt.Fprintf(os.Stdout, "%-52s %s%s\n", i.Identifier, i.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d integration(s)\n", len(ints))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&custom, "custom", false, "show only deletable (custom pack or clone) integrations")
	f.BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// newSOARIntegrationUninstallCmd deletes a CUSTOM integration pack (e.g. a cloned
// "Copy of …") by its addressable key via the v1alpha integrations.delete path.
// Commercial/marketplace packs are not deletable. Guarded LIVE MUTATION.
func newSOARIntegrationUninstallCmd() *cobra.Command {
	var (
		name   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall --name <integration-key>",
		Short: "Delete a custom integration pack (clone) by its key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			target, err := resolveCustomIntegration(ctx, c, name)
			if err != nil {
				return err
			}
			dr, _ := soarGuard("integration uninstall", dryRun, yes)
			key := target.Name
			if key == "" {
				key = target.Identifier
			}
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN: would delete custom integration %q (%s)\n", target.DisplayName, key)
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
	f.StringVar(&name, "name", "", "integration key: Name (clone), Identifier, or displayName (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// resolveCustomIntegration finds the integration addressed by key (matched against
// Name, Identifier, or DisplayName) and refuses anything that isn't custom — the
// guardrail against deleting a commercial pack or the stock base integration.
func resolveCustomIntegration(ctx context.Context, c *soar.Client, key string) (soar.Integration, error) {
	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		return soar.Integration{}, err
	}
	var matches []soar.Integration
	for _, i := range ints {
		if i.Name == key || i.Identifier == key || i.DisplayName == key {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return soar.Integration{}, fmt.Errorf("no installed integration matches %q (try `soar integration list`)", key)
	case 1:
		if !soar.IsDeletableIntegration(matches[0]) {
			return soar.Integration{}, fmt.Errorf("integration %q is a stock base pack, not a custom pack or clone; only those are deletable", key)
		}
		return matches[0], nil
	default:
		return soar.Integration{}, fmt.Errorf("%q is ambiguous (%d matches); address the clone by its unique Name", key, len(matches))
	}
}

func newSOARIntegrationCreateCmd() *cobra.Command {
	var (
		integration string
		env         string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "create --integration <id> --environment <env>",
		Short: "Create a new, unconfigured (inert) integration instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("integration create", dryRun, yes)
			return mirror.PushSOARIntegrationCreate(baseContext(), lc, integration, env, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&env, "environment", "", "environment to scope the instance to (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("environment")
	return cmd
}

func newSOARIntegrationDeleteCmd() *cobra.Command {
	var (
		integration string
		env         string
		id          string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration <id> --environment <env> --id <instance-id>",
		Short: "Delete an integration instance (warns if playbooks use it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("integration delete", dryRun, yes)
			return mirror.PushSOARIntegrationDelete(baseContext(), lc, integration, env, id, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&env, "environment", "", "environment the instance is scoped to (required)")
	f.StringVar(&id, "id", "", "instance identifier to delete (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
