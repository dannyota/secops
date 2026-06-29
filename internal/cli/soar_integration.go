package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		Use:   "integrations",
		Short: "Manage SOAR integration instances (imperative create/delete)",
	}
	cmd.AddCommand(newSOARIntegrationGetCmd(), newSOARIntegrationTestCmd(),
		newSOARIntegrationCreateCmd(), newSOARIntegrationDeleteCmd(),
		newSOARIntegrationConfigureCmd(), newSOARIntegrationListCmd(), newSOARIntegrationInstancesCmd(),
		newSOARIntegrationInstallCmd(), newSOARIntegrationUninstallCmd(), newSOARIntegrationRenameCmd(),
		newSOARIntegrationConnectorCmd(), newSOARIntegrationScaffoldCmd(),
		newSOARIntegrationActionCmd(), newSOARIntegrationJobCmd())
	return cmd
}

// integrationInstance is the subset of an integration's configured instance the
// CLI surfaces: the instance id and the environment a `integrations delete`
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
		return "", "", fmt.Errorf("no matching instance of integration %q (see `integrations instances --integration %s`)", integrationID, integrationID)
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
	var integration string
	cmd := &cobra.Command{
		Use:   "instances --integration <id>",
		Short: "List an integration's configured instances (id · environment · name)",
		Long: "List the configured instances of an installed integration — the instance id\n" +
			"and environment a `integrations delete` needs (which `integration list`,\n" +
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
			if jsonOut {
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
	return markJSON(cmd)
}

func newSOARIntegrationConfigureCmd() *cobra.Command {
	var (
		integration string
		instanceID  string
		env         string
		params      []string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "configure --integration <id> --param key=value …",
		Short: "MUTATING (guarded): set an integration instance's parameters",
		Long: "Read an integration instance's current settings, overlay the given\n" +
			"--param key=value pairs, and save the updated configuration.\n\n" +
			"A secret-valued parameter can reference an environment variable:\n" +
			"  --param 'API_Key=env:MY_SECRET_VAR'\n" +
			"The env var is resolved at apply time; the secret never appears in\n" +
			"shell history (use single-quotes) or in a tracked file.\n\n" +
			"Instance id and environment are auto-resolved when the integration\n" +
			"has a single instance (same as `integration delete`). Guarded: dry-run\n" +
			"by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(integration) == "" {
				return fmt.Errorf("--integration is required")
			}
			if len(params) == 0 {
				return fmt.Errorf("at least one --param key=value is required")
			}
			// Parse --param pairs and resolve env: references.
			overrides := make(map[string]string, len(params))
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok || strings.TrimSpace(k) == "" {
					return fmt.Errorf("invalid --param %q: expected key=value", p)
				}
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				if after, found := strings.CutPrefix(v, "env:"); found {
					envVal := os.Getenv(after)
					if envVal == "" {
						return fmt.Errorf("--param %q references env var %q which is empty or unset", k, after)
					}
					v = envVal
				}
				overrides[k] = v
			}

			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			// Resolve the instance.
			if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(env) == "" {
				if instanceID, env, err = resolveIntegrationInstance(ctx, lc, integration, instanceID, env); err != nil {
					return err
				}
			}

			// Read current settings.
			raw, err := lc.GetIntegrationInstanceSettings(ctx, instanceID)
			if err != nil {
				return err
			}
			items, derr := mirror.DecodeRawList(raw)
			if derr != nil {
				return fmt.Errorf("decode settings: %w", derr)
			}

			// Overlay the --param values on matching propertyName entries.
			type settingEntry struct {
				PropertyName        string `json:"propertyName"`
				PropertyDisplayName string `json:"propertyDisplayName"`
				Value               string `json:"value"`
			}
			// Build a lowercase→original-key index so matching is case-insensitive
			// and works on either propertyName or propertyDisplayName.
			lowerOverrides := make(map[string]string, len(overrides))
			for k := range overrides {
				lowerOverrides[strings.ToLower(k)] = k
			}
			var settings []json.RawMessage
			matchedKeys := map[string]bool{} // user-supplied keys that resolved to a setting
			for _, it := range items {
				var se settingEntry
				if json.Unmarshal(it, &se) != nil {
					settings = append(settings, it)
					continue
				}
				// Match on propertyName or propertyDisplayName (case-insensitive).
				origKey := ""
				for _, cand := range []string{se.PropertyName, se.PropertyDisplayName} {
					if k, ok := lowerOverrides[strings.ToLower(cand)]; ok {
						origKey = k
						break
					}
				}
				if origKey != "" {
					matchedKeys[origKey] = true
					var m map[string]any
					if json.Unmarshal(it, &m) == nil {
						m["value"] = overrides[origKey]
						if b, merr := json.Marshal(m); merr == nil {
							it = b
						}
					}
				}
				settings = append(settings, it)
			}
			// Warn on params that didn't match any existing property.
			for k := range overrides {
				if !matchedKeys[k] {
					fmt.Fprintf(os.Stderr, "warning: --param %q did not match any setting on instance %s\n", k, instanceID)
				}
			}
			if len(matchedKeys) == 0 {
				return fmt.Errorf("no --param values matched any settings on instance %s (available properties are shown by `soar legacy call integrations/GetIntegrationInstanceSettings/%s --read`)", instanceID, instanceID)
			}

			dr, ay := soarGuard("integration configure "+integration, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Instance: %s (integration %s, environment %s)\n", instanceID, integration, env)
			fmt.Fprintf(os.Stdout, "Setting %d parameter(s):", len(matchedKeys))
			for k := range matchedKeys {
				fmt.Fprintf(os.Stdout, " %s", k)
			}
			fmt.Fprintln(os.Stdout)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}

			saveBody := map[string]any{
				"instanceIdentifier": instanceID,
				"settings":           settings,
			}
			if _, serr := lc.SaveStoreIntegrationConfigurationProperties(ctx, saveBody); serr != nil {
				return serr
			}
			fmt.Fprintln(os.Stdout, "configuration saved.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&instanceID, "id", "", "instance identifier (auto-resolved when the integration has one)")
	f.StringVar(&env, "environment", "", "instance environment (auto-resolved)")
	f.StringArrayVar(&params, "param", nil, "key=value to set (repeatable); use env:VAR for secrets")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
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
func newSOARIntegrationConnectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connector",
		Short: "List/delete connector definitions inside an integration",
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

// newSOARIntegrationUninstallCmd deletes a CUSTOM integration pack (e.g. a cloned
// "Copy of …") by its addressable key via the v1alpha integrations.delete path.
// Commercial/marketplace packs are not deletable. Guarded LIVE MUTATION.
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
		return soar.Integration{}, fmt.Errorf("no installed integration matches %q (try `integrations list`)", key)
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
