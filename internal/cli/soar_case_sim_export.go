package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSimExportCmd() *cobra.Command {
	var outFile string
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a custom simulation as JSON",
		Long: "Export a custom simulation case definition as JSON, suitable for\n" +
			"version control or re-import on another tenant.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.AttackSimExportCustomCase(baseContext(), name)
			if err != nil {
				return fmt.Errorf("export simulation %q: %w", name, err)
			}
			if outFile != "" {
				var pretty json.RawMessage
				if json.Unmarshal(raw, &pretty) == nil {
					formatted, ferr := json.MarshalIndent(pretty, "", "  ")
					if ferr == nil {
						raw = formatted
					}
				}
				if werr := os.WriteFile(outFile, append(raw, '\n'), 0o644); werr != nil {
					return werr
				}
				fmt.Fprintf(cmd.OutOrStdout(), "exported %q → %s\n", name, outFile)
				return nil
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().StringVarP(&outFile, "out", "o", "", "write to file instead of stdout")
	return markJSON(cmd)
}

func newSimImportCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "MUTATING (guarded): import a simulation from a JSON file",
		Long: "Import a custom simulation case from a JSON file (as exported by\n" +
			"`simulation export`). Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var body json.RawMessage
			if err := json.Unmarshal(data, &body); err != nil {
				return fmt.Errorf("invalid JSON in %s: %w", args[0], err)
			}
			label := fmt.Sprintf("simulation import %s", args[0])
			return soarGuardedMutation(label, dryRun, yes, func() error {
				lc, err := newSOARLegacyClient()
				if err != nil {
					return err
				}
				_, err = lc.AttackSimImportCustomCase(baseContext(), body)
				if err != nil {
					return fmt.Errorf("import simulation: %w", err)
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
