package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
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
		newSOARIntegrationListCmd(), newSOARIntegrationInstancesCmd(), newSOARIntegrationInstallCmd(),
		newSOARIntegrationUninstallCmd(), newSOARIntegrationConnectorCmd(), newSOARIntegrationScaffoldCmd())
	return cmd
}

// integrationInstance is the subset of an integration's configured instance the
// CLI surfaces: the instance id and the environment a `soar integration delete`
// needs, plus a human name.
type integrationInstance struct {
	Identifier            string `json:"identifier"`
	IntegrationIdentifier string `json:"integrationIdentifier"`
	EnvironmentIdentifier string `json:"environmentIdentifier"`
	InstanceName          string `json:"instanceName"`
	IsConfigured          bool   `json:"isConfigured"`
}

// listIntegrationInstances returns one integration's instances across every
// environment (id + environment + name) — the fields `integration delete` needs
// but `integration list` (packs only) does not expose.
func listIntegrationInstances(ctx context.Context, lc *legacy.Client, integrationID string) ([]integrationInstance, error) {
	envRaw, err := lc.AgentListAvailableEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	// Both responses are legacy lists; decode tolerantly (bare array or wrapped)
	// the same way PushSOARIntegrationDelete decodes this very endpoint.
	envItems, err := mirror.DecodeRawList(envRaw)
	if err != nil {
		return nil, fmt.Errorf("decode environments: %w", err)
	}
	envs := make([]string, 0, len(envItems))
	for _, e := range envItems {
		var s string
		if json.Unmarshal(e, &s) == nil && s != "" {
			envs = append(envs, s)
		}
	}
	raw, err := lc.ListOptionalIntegrationInstances(ctx, map[string]any{
		"environments": envs, "integrationIdentifier": integrationID,
	})
	if err != nil {
		return nil, err
	}
	items, err := mirror.DecodeRawList(raw)
	if err != nil {
		return nil, fmt.Errorf("decode integration instances: %w", err)
	}
	insts := make([]integrationInstance, 0, len(items))
	for _, it := range items {
		var in integrationInstance
		if json.Unmarshal(it, &in) == nil {
			insts = append(insts, in)
		}
	}
	return insts, nil
}

// resolveIntegrationInstance narrows an integration's instances by an optional id
// and/or environment to a single (id, environment) for a delete. Exactly one match
// resolves; zero or several error with a legible, copy-pasteable message.
func resolveIntegrationInstance(ctx context.Context, lc *legacy.Client, integrationID, wantID, wantEnv string) (id, env string, err error) {
	insts, err := listIntegrationInstances(ctx, lc, integrationID)
	if err != nil {
		return "", "", err
	}
	return pickIntegrationInstance(insts, integrationID, wantID, wantEnv)
}

// pickIntegrationInstance narrows a set of instances by an optional id/environment
// to a single (id, environment). Pure, so the match/ambiguity logic is unit-tested
// without a live client.
func pickIntegrationInstance(insts []integrationInstance, integrationID, wantID, wantEnv string) (id, env string, err error) {
	var matches []integrationInstance
	for i := range insts {
		in := &insts[i]
		if wantID != "" && in.Identifier != wantID {
			continue
		}
		if wantEnv != "" && !strings.EqualFold(in.EnvironmentIdentifier, wantEnv) {
			continue
		}
		matches = append(matches, *in)
	}
	switch len(matches) {
	case 1:
		return matches[0].Identifier, matches[0].EnvironmentIdentifier, nil
	case 0:
		return "", "", fmt.Errorf("no matching instance of integration %q (see `soar integration instances --integration %s`)", integrationID, integrationID)
	default:
		var b strings.Builder
		for i := range matches {
			m := &matches[i]
			fmt.Fprintf(&b, "\n  --id %s --environment %q  (%s)", m.Identifier, m.EnvironmentIdentifier, m.InstanceName)
		}
		return "", "", fmt.Errorf("integration %q has %d instances — narrow with --id / --environment:%s", integrationID, len(matches), b.String())
	}
}

func newSOARIntegrationInstancesCmd() *cobra.Command {
	var (
		integration string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "instances --integration <id>",
		Short: "List an integration's configured instances (id · environment · name)",
		Long: "List the configured instances of an installed integration — the instance id\n" +
			"and environment a `soar integration delete` needs (which `integration list`,\n" +
			"showing packs only, does not expose).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(integration) == "" {
				return fmt.Errorf("--integration is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			insts, err := listIntegrationInstances(baseContext(), lc, integration)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(insts)
			}
			for i := range insts {
				in := &insts[i]
				fmt.Fprintf(os.Stdout, "%-40s %-24s %s\n", in.Identifier, in.EnvironmentIdentifier, in.InstanceName)
			}
			fmt.Fprintf(os.Stdout, "\n%d instance(s) of %s\n", len(insts), integration)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
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
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
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
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
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
		Use:   "delete --integration <id> [--environment <env>] [--id <instance-id>]",
		Short: "Delete an integration instance (warns if playbooks use it)",
		Long: "Delete an integration instance. When `--id` / `--environment` are omitted they\n" +
			"are resolved from the integration's instances — a single instance is selected\n" +
			"automatically; several list themselves so you can narrow. Guarded: dry-run by\n" +
			"default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(integration) == "" {
				return fmt.Errorf("--integration is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			// Resolve the instance from the integration when the id or environment is
			// not given (the common case — `integration list` shows only the pack).
			if strings.TrimSpace(id) == "" || strings.TrimSpace(env) == "" {
				if id, env, err = resolveIntegrationInstance(baseContext(), lc, integration, id, env); err != nil {
					return err
				}
			}
			dr, ay := soarGuard("integration delete", dryRun, yes)
			return mirror.PushSOARIntegrationDelete(baseContext(), lc, integration, env, id, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&env, "environment", "", "environment the instance is scoped to (resolved from --integration when omitted)")
	f.StringVar(&id, "id", "", "instance identifier to delete (resolved when the integration has one instance)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
