package cli

import (
	"fmt"
	"os"
	"path"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `parsers` command operates parser versions directly: list a log type's
// versions, test a parser's CBN against sample logs (no server change), and
// activate a specific version. Parser config-as-code lives in
// `pull parsers` / `push parsers` (which creates a new version and activates it).
func init() { rootCmd.AddCommand(newParsersCmd()) }

func newParsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parsers <verb>",
		Short: "Inspect and activate log parsers (versions / run / activate)",
		Long: "Operate parser versions directly:\n" +
			"  versions  list a log type's parser versions (id, state, created)\n" +
			"  run       validate a CBN parser against sample logs (no server change)\n" +
			"  activate  make a specific parser version ACTIVE (guarded)\n\n" +
			"Config-as-code (edit + create-new-version + activate) is `push parsers`.",
	}
	cmd.AddCommand(newParsersVersionsCmd(), newParsersRunCmd(), newParsersActivateCmd())
	return cmd
}

// parserID is the trailing id segment of a parser resource name.
func parserID(name string) string { return path.Base(name) }

func newParsersVersionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions <log-type>",
		Short: "Read-only: list a log type's parser versions (id, state, created)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ps, err := c.ListParsers(baseContext(), args[0])
			if err != nil {
				return err
			}
			type row struct {
				ParserID   string `json:"parser_id"`
				State      string `json:"state"`
				Type       string `json:"type,omitempty"`
				CreateTime string `json:"create_time,omitempty"`
			}
			rows := make([]row, 0, len(ps))
			for i := range ps {
				rows = append(rows, row{ParserID: parserID(ps[i].Name), State: ps[i].State, Type: ps[i].Type, CreateTime: ps[i].CreateTime})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].CreateTime > rows[j].CreateTime })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PARSER ID\tSTATE\tTYPE\tCREATED")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ParserID, r.State, r.Type, r.CreateTime)
			}
			return tw.Flush()
		},
	}
	return cmd
}

func newParsersRunCmd() *cobra.Command {
	var cbnFile, logsFile string
	cmd := &cobra.Command{
		Use:   "run <log-type> --cbn <file> --logs <file>",
		Short: "Validate a CBN parser against sample logs (no server change)",
		Long: "Run a local parser's CBN against sample log lines and print the parsed UDM.\n" +
			"Purely inert — it creates and activates nothing — so it's safe to run before\n" +
			"`push parsers` (which would create a new version and activate it).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := os.ReadFile(cbnFile)
			if err != nil {
				return fmt.Errorf("read --cbn: %w", err)
			}
			logs, err := readLines(cmd, logsFile) // verbatim lines ('-' = stdin); # and blanks are real log content
			if err != nil {
				return fmt.Errorf("read --logs: %w", err)
			}
			if len(logs) == 0 {
				return fmt.Errorf("no sample logs in %q", logsFile)
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			resp, err := c.RunParser(baseContext(), args[0], string(code), logs)
			if err != nil {
				return err
			}
			return emitJSON(resp) // the parsed UDM is inherently structured output
		},
	}
	cmd.Flags().StringVar(&cbnFile, "cbn", "", "parser source (CBN) file to test (required)")
	cmd.Flags().StringVar(&logsFile, "logs", "", "sample log lines, one per line ('-' for stdin) (required)")
	_ = cmd.MarkFlagRequired("cbn")
	_ = cmd.MarkFlagRequired("logs")
	return cmd
}

func newParsersActivateCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "activate <log-type> <parser-id>",
		Short: "Make a parser version ACTIVE (guarded; live ingestion switches)",
		Long: "Activate a specific parser version for a log type — live ingestion switches to\n" +
			"it immediately. Guarded: dry-run by default, --yes to apply. Use `parsers\n" +
			"versions` to find a prior version's id to roll back to.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType, pid := args[0], args[1]
			target := fmt.Sprintf("activate parser %s/%s", logType, pid)
			dr, ay := soarGuard(target, dryRun, yes) // generic dry-run/--yes guard
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would activate parser %s for %q. Re-run with --yes.\n", pid, logType)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to activate without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if err := c.ActivateParser(baseContext(), logType, pid); err != nil {
				return err
			}
			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Activated parser %s for %q.\n", pid, logType)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
