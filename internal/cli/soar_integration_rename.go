package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// newSOARIntegrationListCmd lists installed integration packs via the modern
// v1alpha surface — the discovery side of uninstall. Read-only.
func newSOARIntegrationListCmd() *cobra.Command {
	var (
		custom    bool
		instances bool
	)
	cmd := &cobra.Command{
		Use:   "list [--custom] [--instances]",
		Short: "List installed integration packs (read-only)",
		Long: "List installed integration packs. With --instances, also show the\n" +
			"configured instances under each pack (environment + display name).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ints, err := c.ListIntegrations(ctx)
			if err != nil {
				return err
			}
			if custom {
				ints = slices.DeleteFunc(ints, func(i soar.Integration) bool { return !soar.IsDeletableIntegration(i) })
			}

			var allInstances []soar.IntegrationInstance
			if instances {
				allInstances, err = c.ListAllIntegrationInstances(ctx)
				if err != nil {
					return err
				}
			}

			if jsonOut {
				if !instances {
					return emitJSON(ints)
				}
				type packWithInstances struct {
					soar.Integration
					Instances []soar.IntegrationInstance `json:"instances"`
				}
				out := make([]packWithInstances, 0, len(ints))
				for _, i := range ints {
					p := packWithInstances{Integration: i}
					for _, inst := range allInstances {
						if inst.IntegrationIdentifier == i.Identifier {
							p.Instances = append(p.Instances, inst)
						}
					}
					out = append(out, p)
				}
				return emitJSON(out)
			}

			instByPack := map[string][]soar.IntegrationInstance{}
			for _, inst := range allInstances {
				instByPack[inst.IntegrationIdentifier] = append(instByPack[inst.IntegrationIdentifier], inst)
			}

			for _, i := range ints {
				tag := ""
				if soar.IsDeletableIntegration(i) {
					tag = "  [deletable]"
				}
				fmt.Fprintf(os.Stdout, "%-52s %s%s\n", i.Identifier, i.DisplayName, tag)
				for _, inst := range instByPack[i.Identifier] {
					customTag := ""
					if !inst.SystemDefault {
						customTag = "  [renamable]"
					}
					fmt.Fprintf(os.Stdout, "    %-36s %-20s %s%s\n", inst.Identifier, inst.Environment, inst.DisplayName, customTag)
				}
			}
			fmt.Fprintf(os.Stdout, "\n%d integration(s)", len(ints))
			if instances {
				fmt.Fprintf(os.Stdout, ", %d instance(s)", len(allInstances))
			}
			fmt.Fprintln(os.Stdout)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&custom, "custom", false, "show only deletable (custom pack or clone) integrations")
	f.BoolVar(&instances, "instances", false, "also show configured instances under each pack")
	return markJSON(cmd)
}

func newSOARIntegrationRenameCmd() *cobra.Command {
	var (
		integration string
		instanceID  string
		env         string
		newName     string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "rename --integration <id> (--instance <uuid> | --env <env>) --name <new-name>",
		Short: "MUTATING (guarded): rename an integration instance",
		Long: "Rename an integration instance's displayName via the v1alpha PATCH surface.\n" +
			"Identify the instance by its UUID (--instance) or by integration + environment\n" +
			"(--env resolves to the single instance in that environment).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(newName) == "" {
				return fmt.Errorf("--name is required")
			}
			if strings.TrimSpace(integration) == "" {
				return fmt.Errorf("--integration is required")
			}

			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			instID := strings.TrimSpace(instanceID)
			if instID == "" {
				if strings.TrimSpace(env) == "" {
					return fmt.Errorf("--instance or --env is required")
				}
				instances, err := mc.ListIntegrationInstances(ctx, integration, env)
				if err != nil {
					return err
				}
				if len(instances) == 0 {
					return fmt.Errorf("no instance of %q found in environment %q", integration, env)
				}
				if len(instances) > 1 {
					return fmt.Errorf("multiple instances of %q in environment %q — use --instance to pick one", integration, env)
				}
				instID = instances[0].Identifier
			}

			isDry, _ := soarGuard(fmt.Sprintf("rename integration instance %s → %q", instID, newName), dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would rename instance %s of %s to %q\n", instID, integration, newName)
				return nil
			}

			raw, err := mc.UpdateIntegrationInstance(ctx, integration, instID,
				map[string]any{"displayName": newName},
				"displayName",
			)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed instance %s → %q\n", instID, newName)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration identifier/key (required)")
	f.StringVar(&instanceID, "instance", "", "instance UUID")
	f.StringVar(&env, "env", "", "environment name (resolves to the instance in that env)")
	f.StringVar(&newName, "name", "", "new display name (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.MarkFlagsMutuallyExclusive("instance", "env")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
