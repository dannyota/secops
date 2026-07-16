package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
)

// `soar integration configure`: create-or-update an integration instance and
// its parameters in one guarded verb.

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

			if !jsonOut {
				fmt.Fprintf(os.Stdout, "Instance: %s (integration %s, environment %s)\n", instanceID, integration, env)
				fmt.Fprintf(os.Stdout, "Setting %d parameter(s):", len(matchedKeys))
				for k := range matchedKeys {
					fmt.Fprintf(os.Stdout, " %s", k)
				}
				fmt.Fprintln(os.Stdout)
			}

			return soarGuardedMutation("integration configure "+integration, dryRun, yes, func() error {
				saveBody := map[string]any{
					"instanceIdentifier": instanceID,
					"settings":           settings,
				}
				if _, serr := lc.SaveStoreIntegrationConfigurationProperties(ctx, saveBody); serr != nil {
					return serr
				}
				return nil
			})
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
