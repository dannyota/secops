package cli

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newDataTablesCmd()) }

func newDataTablesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-tables <verb>",
		Short: "Manage data tables (import rows from CSV)",
	}
	cmd.AddCommand(newDataTablesImportCmd())
	return cmd
}

func newDataTablesImportCmd() *cobra.Command {
	var (
		table      string
		replace    bool
		skipHeader bool
		dryRun     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "import [--table <name>] [--replace] [--skip-header] <file.csv>",
		Short: "MUTATING (guarded): import rows from a CSV file into a data table",
		Long: "Parse a CSV file and bulk-import its rows into the named data table.\n" +
			"By default rows are appended; use --replace to clear the table first.\n" +
			"The first row is skipped as a header by default (--skip-header=false to keep it).\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			table = strings.TrimSpace(table)
			if table == "" {
				return fmt.Errorf("--table is required")
			}
			path := args[0]

			rows, err := readCSV(path, skipHeader)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(os.Stdout, "no rows to import.")
				return nil
			}

			mode := "append"
			if replace {
				mode = "replace"
			}
			action := fmt.Sprintf("data-tables import %d row(s) into %q [%s]", len(rows), table, mode)

			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				ctx := baseContext()

				// Verify the table exists before mutating.
				if _, err := c.GetDataTable(ctx, table); err != nil {
					return fmt.Errorf("table %q: %w", table, err)
				}

				var n int
				if replace {
					batches, err := c.ReplaceDataTableRows(ctx, table, rows)
					if err != nil {
						return err
					}
					for _, b := range batches {
						n += len(b.DataTableRows)
					}
				} else {
					batches, err := c.CreateDataTableRows(ctx, table, rows)
					if err != nil {
						return err
					}
					for _, b := range batches {
						n += len(b.DataTableRows)
					}
				}
				fmt.Fprintf(os.Stdout, "imported %d row(s) into %q.\n", n, table)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&table, "table", "", "data table name (required)")
	f.BoolVar(&replace, "replace", false, "replace all existing rows instead of appending")
	f.BoolVar(&skipHeader, "skip-header", true, "skip the first CSV row (header)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("table")
	return markJSON(cmd)
}

func readCSV(path string, skipHeader bool) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer func() { _ = file.Close() }()

	all, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv: %w", err)
	}
	if skipHeader && len(all) > 0 {
		all = all[1:]
	}
	return all, nil
}
