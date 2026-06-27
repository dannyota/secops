package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// groupingModuleSettings is the moduleSettings name for the alert-grouping
// General/Overflow tuning knobs (Timeframe, co-grouping, overflow, max-alerts).
const groupingModuleSettings = "AlertGroupingSettings"

// newSOARGroupingSettingsCmd manages the alert-grouping General/Overflow settings
// singleton through the modern moduleSettings property bag (v1alpha, SOAR host).
// `soar push grouping` reconciles the grouping RULES; this is the settings
// singleton the rule push can't reach, so the tuning knobs are config-as-code too
// instead of UI-only. Imperative (one record, no id/list/delete).
func newSOARGroupingSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grouping <get|set>",
		Short: "Read/set the alert-grouping General/Overflow settings (Timeframe, co-grouping, overflow)",
	}
	cmd.AddCommand(newGroupingSettingsGetCmd(), newGroupingSettingsSetCmd())
	return cmd
}

func newGroupingSettingsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Read the alert-grouping settings (max-alerts-per-case + any module properties)",
		Long: "Read the alert-grouping General/Overflow settings from the modern moduleSettings\n" +
			"property bag — Timeframe, max-alerts, overflow timeframe/max, grouping algorithm,\n" +
			"and source-grouping-identifier fallback (the same properties the SOAR Settings >\n" +
			"Alerts Grouping page shows) — plus the legacy max-alerts-per-case value.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := baseContext()
			// Reliable source: the legacy max-alerts-per-case config value.
			var maxAlerts string
			if lc, lerr := newSOARLegacyClient(); lerr == nil {
				if raw, gerr := lc.SettingXGetMaximumAlertsGroupingConfiguration(ctx); gerr == nil {
					maxAlerts = strings.TrimSpace(string(raw))
				}
			}
			// Modern moduleSettings property bag (the writable knobs where exposed).
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			props, err := c.ListModuleSettingProperties(ctx, groupingModuleSettings)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(struct {
					MaximumAlertsGroupingConfiguration string                       `json:"maximumAlertsGroupingConfiguration,omitempty"`
					Properties                         []soar.ModuleSettingProperty `json:"properties"`
				}{maxAlerts, props})
			}
			if maxAlerts != "" {
				fmt.Printf("  %-44s = %s\n", "maximumAlertsGroupingConfiguration", maxAlerts)
			}
			sort.Slice(props, func(i, j int) bool { return props[i].ShortName() < props[j].ShortName() })
			for _, p := range props {
				fmt.Printf("  %-44s = %s\n", p.ShortName(), p.Value)
			}
			if maxAlerts == "" && len(props) == 0 {
				fmt.Println("no alert-grouping settings returned by this instance.")
			}
			return nil
		},
	}
	return markJSON(cmd)
}

func newGroupingSettingsSetCmd() *cobra.Command {
	var (
		propsArg []string
		dryRun   bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "set --property <name>=<value> [--property ...]",
		Short: "MUTATING (guarded): set alert-grouping settings properties",
		Long: "Set one or more alert-grouping settings properties by name=value, via the\n" +
			"modern moduleSettings bag — the same properties the SOAR Settings > Alerts\n" +
			"Grouping page edits. Property names come from `soar settings grouping get`\n" +
			"(e.g. TimeframeForGroupingInHours, MaxAGroupingForAlerts, OverflowMaxAGroupingForAlerts,\n" +
			"FallbackToSourceGroupingIdentifier). The dry-run preview shows each current ->\n" +
			"requested transition; unchanged properties are skipped. Guarded: dry-run by\n" +
			"default, --yes to apply (v1alpha on the SOAR host).",
		Example: "  secopsctl soar settings grouping get\n" +
			"  secopsctl soar settings grouping set --property TimeframeForGroupingInHours=4 --dry-run",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			want, err := parseKeyValues(propsArg)
			if err != nil {
				return err
			}
			if len(want) == 0 {
				return fmt.Errorf("pass at least one --property <name>=<value>")
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			live, err := c.ListModuleSettingProperties(ctx, groupingModuleSettings)
			if err != nil {
				return err
			}
			current := map[string]string{}
			liveByShort := map[string]soar.ModuleSettingProperty{}
			for _, p := range live {
				current[p.ShortName()] = p.Value
				liveByShort[p.ShortName()] = p
			}
			// Validate names against the live set and compute the changed subset, so
			// a typo'd property fails before the guard and a no-op is reported as such.
			var changed []soar.ModuleSettingProperty
			names := make([]string, 0, len(want))
			for k := range want {
				names = append(names, k)
			}
			sort.Strings(names)
			// Preview to stderr so a --json stdout stays clean.
			for _, name := range names {
				lp, ok := liveByShort[name]
				if !ok {
					return fmt.Errorf("unknown grouping setting %q — see `soar settings grouping get` for valid names", name)
				}
				if cur, ok := current[name]; ok && cur == want[name] {
					fmt.Fprintf(os.Stderr, "note: %s already = %q (no change)\n", name, want[name])
					continue
				}
				fmt.Fprintf(os.Stderr, "  %s: %q -> %q\n", name, current[name], want[name])
				// Write keys on the full resource name the batchUpdate RPC expects.
				changed = append(changed, soar.ModuleSettingProperty{Name: lp.Name, Value: want[name]})
			}
			action := "set grouping settings"
			if len(changed) == 0 {
				fmt.Fprintln(os.Stderr, "nothing to change — all requested values already match.")
				if jsonOut {
					return emitGuardedResult(action, false, false)
				}
				return nil
			}
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				if jsonOut {
					return emitGuardedResult(action, dr, false)
				}
				if dr {
					fmt.Fprintln(os.Stdout, "DRY RUN — no changes applied. Re-run with --yes to apply.")
				}
				return nil
			}
			if _, err = c.BatchUpdateModuleSettingProperties(ctx, groupingModuleSettings, changed); err != nil {
				return err
			}
			if jsonOut {
				return emitGuardedResult(action, false, true)
			}
			fmt.Fprintf(os.Stdout, "Done: %s (%d propert(ies)).\n", action, len(changed))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&propsArg, "property", nil, "a grouping setting as name=value (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// parseKeyValues parses repeated name=value flags into a map, erroring on a
// malformed entry or an empty name.
func parseKeyValues(items []string) (map[string]string, error) {
	out := map[string]string{}
	for _, it := range items {
		k, v, ok := strings.Cut(it, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --property %q (want name=value)", it)
		}
		out[k] = v
	}
	return out, nil
}
