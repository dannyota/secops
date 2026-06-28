package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCaseSimulationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulation <verb>",
		Short: "Playbook development test harness: create/list/simulate/delete custom test cases",
		Long: "Manage the custom-case simulation harness for playbook development. Test\n" +
			"cases appear in the SOAR case queue so playbooks can be attached, debugged,\n" +
			"and iterated without real incidents.",
	}
	cmd.AddCommand(
		newSimListCmd(),
		newSimGetCmd(),
		newSimCreateCmd(),
		newSimGenerateCmd(),
		newSimAlertCmd(),
		newSimDeleteCmd(),
		newSimExportCmd(),
		newSimImportCmd(),
	)
	return cmd
}

func newSimListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List custom simulation names",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return preferModern("soar case simulation list",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.GetCustomCases(baseContext())
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "custom simulations", raw)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.AttackSimGetCustomCases(baseContext())
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "custom simulations", raw)
					return nil
				},
			)
		},
	}
	return markJSON(cmd)
}

func newSimGetCmd() *cobra.Command {
	var (
		name string
		env  string
	)
	cmd := &cobra.Command{
		Use:   "get --name <simulation>",
		Short: "Read one simulation's config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if strings.TrimSpace(env) == "" {
				env = "Default Environment"
			}
			body := map[string]any{
				"customCases": []string{name},
				"environment": env,
				"kinds":       []any{},
			}
			return preferModern("soar case simulation get",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.GetCustomCaseDetails(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "simulation detail", raw)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.AttackSimGetCustomCaseDetails(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "simulation detail", raw)
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "custom simulation name (required)")
	f.StringVar(&env, "environment", "Default Environment", "SOAR environment")
	return markJSON(cmd)
}

func newSimCreateCmd() *cobra.Command {
	var (
		alertSource  string
		ruleName     string
		alertProduct string
		alertName    string
		eventName    string
		eventFields  []string
		alertFields  []string
		dryRun       bool
		yes          bool
	)
	cmd := &cobra.Command{
		Use:   "create --alert-name <name> --rule-name <rule> --alert-source <src> --alert-product <prod> --event-name <evt>",
		Short: "MUTATING (guarded): create a custom simulated test case",
		Long: "Create a custom (simulated) test case from alert and event field specs.\n" +
			"The case appears in the SOAR queue for playbook attachment and testing.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, kv := range []struct{ name, val string }{
				{"--alert-name", alertName},
				{"--rule-name", ruleName},
				{"--alert-source", alertSource},
				{"--alert-product", alertProduct},
				{"--event-name", eventName},
			} {
				if strings.TrimSpace(kv.val) == "" {
					return fmt.Errorf("%s is required", kv.name)
				}
			}
			body := map[string]any{
				"alertSource":  alertSource,
				"ruleName":     ruleName,
				"alertProduct": alertProduct,
				"alertName":    alertName,
				"eventName":    eventName,
			}
			if kvs := parseSimKVFields(eventFields); len(kvs) > 0 {
				body["additionalEventFields"] = kvs
			}
			if kvs := parseSimKVFields(alertFields); len(kvs) > 0 {
				body["additionalAlertFields"] = kvs
			}
			label := fmt.Sprintf("simulation create %q", alertName)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Simulation: %s (rule %s, source %s, product %s, event %s)\n",
				alertName, ruleName, alertSource, alertProduct, eventName)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			return preferModern("soar case simulation create",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					_, err = mc.CreateSimulatedCustomCase(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "created simulation %q\n", alertName)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					_, err = lc.AttackSimCreateSimulatedCustomCase(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "created simulation %q\n", alertName)
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&alertName, "alert-name", "", "alert name for the simulation (required)")
	f.StringVar(&ruleName, "rule-name", "", "rule name that triggers the alert (required)")
	f.StringVar(&alertSource, "alert-source", "", "alert source (required)")
	f.StringVar(&alertProduct, "alert-product", "", "alert product (required)")
	f.StringVar(&eventName, "event-name", "", "event name (required)")
	f.StringArrayVar(&eventFields, "event-field", nil, "additional event fields as key=value (repeatable)")
	f.StringArrayVar(&alertFields, "alert-field", nil, "additional alert fields as key=value (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func newSimGenerateCmd() *cobra.Command {
	var (
		env    string
		names  []string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "generate --environment <env> --name <simulation> …",
		Short: "MUTATING (guarded): generate test cases from custom simulation names",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(names) == 0 {
				return fmt.Errorf("at least one --name is required")
			}
			if strings.TrimSpace(env) == "" {
				env = "Default Environment"
			}
			body := map[string]any{
				"environment": env,
				"customCases": names,
				"kinds":       []any{},
			}
			label := fmt.Sprintf("simulation generate %d case(s) in %s", len(names), env)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Generate: %d simulation(s) in %q\n", len(names), env)
			for _, n := range names {
				fmt.Fprintf(os.Stdout, "  - %s\n", n)
			}
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			return preferModern("soar case simulation generate",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.GenerateUseCases(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					fmt.Fprintln(os.Stdout, "generated.")
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.AttackSimGenerateUseCases(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					fmt.Fprintln(os.Stdout, "generated.")
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&env, "environment", "Default Environment", "SOAR environment")
	f.StringArrayVar(&names, "name", nil, "custom simulation name to generate (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newSimAlertCmd() *cobra.Command {
	var (
		caseID int
		alert  string
		env    string
		group  bool
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "alert --case-id N --alert <id>",
		Short: "MUTATING (guarded): simulate an alert inside a case for playbook testing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			if strings.TrimSpace(alert) == "" {
				return fmt.Errorf("--alert is required")
			}
			body := map[string]any{
				"caseId":           caseID,
				"alertIdentifier":  alert,
				"shouldDoGrouping": group,
			}
			if env != "" {
				body["environment"] = env
			}
			label := fmt.Sprintf("simulation alert on case %d", caseID)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Simulate alert on case %d (alert %s)\n", caseID, truncate(alert, 60))
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			return preferModern("soar case simulation alert",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					_, err = mc.SimulateAlert(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, "alert simulated.")
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					_, err = lc.AttackSimSimulateAlert(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, "alert simulated.")
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&alert, "alert", "", "alert identifier (required)")
	f.StringVar(&env, "environment", "", "environment (optional)")
	f.BoolVar(&group, "group", false, "perform grouping on the simulated alert")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func parseSimKVFields(pairs []string) []map[string]string {
	var out []map[string]string
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		out = append(out, map[string]string{"key": strings.TrimSpace(k), "value": v})
	}
	return out
}

func newSimDeleteCmd() *cobra.Command {
	var (
		name   string
		env    string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete --name <simulation>",
		Short: "MUTATING (guarded): delete a custom simulation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if strings.TrimSpace(env) == "" {
				env = "Default Environment"
			}
			body := map[string]any{
				"customCases": []string{name},
				"environment": env,
				"kinds":       []any{},
			}
			label := fmt.Sprintf("simulation delete %q", name)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Delete simulation: %s\n", name)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			return preferModern("soar case simulation delete",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					_, err = mc.DeleteUseCase(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "deleted simulation %q\n", name)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					_, err = lc.AttackSimDeleteUseCase(baseContext(), body)
					if err != nil {
						return err
					}
					fmt.Fprintf(os.Stdout, "deleted simulation %q\n", name)
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "custom simulation name (required)")
	f.StringVar(&env, "environment", "Default Environment", "SOAR environment")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
