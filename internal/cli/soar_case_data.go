package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCaseCustomFieldsCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "custom-fields --case-id N",
		Short: "List custom field values on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListCustomFieldValues(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "custom field values", raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseWallCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "wall --case-id N",
		Short: "List case wall timeline records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListCaseWallRecords(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "case wall records", raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <verb>",
		Short: "Case-level key-value context properties (playbook state)",
	}
	cmd.AddCommand(newCaseContextListCmd(), newCaseContextSetCmd())
	return cmd
}

func newCaseContextListCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "list --case-id N",
		Short: "List context properties on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListContextProperties(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "context properties", raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseContextSetCmd() *cobra.Command {
	var (
		caseID int
		key    string
		value  string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "set --case-id N --key <k> --value <v>",
		Short: "MUTATING (guarded): set a context property on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" {
				return fmt.Errorf("--key is required")
			}
			label := fmt.Sprintf("case %d context set %s", caseID, key)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Case: %d\nKey: %s\nValue: %s\n", caseID, key, value)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			body := map[string]any{"key": key, "value": value}
			_, err = mc.SetContextProperty(baseContext(), caseID, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "context property %q set on case %d.\n", key, caseID)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&key, "key", "", "property key (required)")
	f.StringVar(&value, "value", "", "property value")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
