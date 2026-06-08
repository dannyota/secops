package cli

import (
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
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
			"  sample-logs  fetch a sample of RAW logs for a log type (to develop against)\n" +
			"  versions     list a log type's parser versions (id, state, created)\n" +
			"  run          validate a CBN parser against sample logs (no server change)\n" +
			"  validate     show parsing errors from a submitted parser's validation report\n" +
			"  activate     make a specific parser version ACTIVE (guarded)\n\n" +
			"Parser-dev loop: sample-logs → write CBN → run → `push parsers` (submit) →\n" +
			"validate (why a submit's FAILED_PRECONDITION failed). Config-as-code is\n" +
			"`push parsers` (edit + create-new-version + activate).",
	}
	cmd.AddCommand(newParsersVersionsCmd(), newParsersRunCmd(), newParsersActivateCmd(),
		newParsersSampleLogsCmd(), newParsersValidateCmd())
	return cmd
}

// newParsersValidateCmd surfaces the parsing errors from a submitted parser's
// validation report — the detail behind a `push parsers` / `parsers activate`
// FAILED_PRECONDITION, which otherwise gives no reason.
func newParsersValidateCmd() *cobra.Command {
	var (
		limit    int
		showLogs bool
	)
	cmd := &cobra.Command{
		Use:   "validate <log-type>",
		Short: "Show parsing errors from a submitted parser's validation report (why a submit failed)",
		Long: "After `push parsers` / `parsers activate` fails with FAILED_PRECONDITION (a\n" +
			"validation failure with no detail), this surfaces WHY: the most recently\n" +
			"submitted parser's validation report and its parsing errors — the per-log error\n" +
			"message plus a preview of the failing raw log (--show-logs for the full sample).\n" +
			"Closes the parser-dev loop in-tool.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			if logType == "" {
				return fmt.Errorf("a LOG_TYPE is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ps, err := c.ListParsers(baseContext(), logType)
			if err != nil {
				return err
			}
			// The parser carrying a validation report is the submitted one; pick the
			// most recent if several have run validation.
			var target *chronicle.Parser
			for i := range ps {
				if ps[i].ValidationReport == "" {
					continue
				}
				// Most recent by createTime (RFC3339 Z → lexicographic = chronological);
				// a populated time always beats an empty one, so an unset createTime can't
				// shadow a real candidate.
				switch {
				case target == nil, target.CreateTime == "":
					target = &ps[i]
				case ps[i].CreateTime != "" && ps[i].CreateTime > target.CreateTime:
					target = &ps[i]
				}
			}
			if target == nil {
				fmt.Fprintf(os.Stderr, "no validation report for %q — nothing recently submitted, or it validated cleanly\n", logType)
				return nil
			}
			errs, err := c.ListParsingErrors(baseContext(), target.ValidationReport, limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(struct {
					ParserID        string                   `json:"parser_id"`
					ValidationStage string                   `json:"validation_stage"`
					ParsingErrors   []chronicle.ParsingError `json:"parsing_errors"`
				}{parserID(target.Name), target.ValidationStage, errs})
			}
			fmt.Printf("Parser %s — validation stage: %s — %d parsing error(s)\n\n",
				parserID(target.Name), dashIfEmpty(target.ValidationStage), len(errs))
			for i, e := range errs {
				fmt.Printf("  [%d] %s\n", i+1, e.Message())
				if sample := decodeLogData(e.LogData); sample != "" {
					if showLogs {
						fmt.Printf("      log: %s\n", sample)
					} else {
						fmt.Printf("      log: %s\n", truncate(sample, 120))
					}
				}
			}
			if len(errs) == 0 {
				fmt.Println("  (no parsing errors listed — the failure may be elsewhere; check `parsers versions`)")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&limit, "limit", 100, "max parsing errors to fetch")
	f.BoolVar(&showLogs, "show-logs", false, "print the full failing raw log per error (default: a 120-char preview)")
	return cmd
}

// decodeLogData base64-decodes a ParsingError.logData to text (verbatim fallback),
// trimming a trailing newline.
func decodeLogData(s string) string {
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
		return strings.TrimRight(string(dec), "\r\n")
	}
	return s
}

// newParsersSampleLogsCmd fetches recent raw sample logs for a log type directly
// (logTypes/<type>/logs) — the simplest raw-log path, no search.
func newParsersSampleLogsCmd() *cobra.Command {
	var (
		limit int
		since time.Duration
	)
	cmd := &cobra.Command{
		Use:   "sample-logs <log-type>",
		Short: "Fetch a sample of RAW logs for a log type (to develop/validate a parser)",
		Long: "List a sample of a log type's raw (ingested) logs directly (logTypes/<type>/logs)\n" +
			"and print each one's FULL raw bytes, one per line — the sample to develop or\n" +
			"validate a parser against. Pipe into a parser test:\n\n" +
			"  secopsctl parsers sample-logs KONG_GATEWAY --limit 50 | \\\n" +
			"    secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -\n\n" +
			"The simplest raw-log path — a direct list, no search (cf. `query udm --raw` /\n" +
			"`query raw`, which scope by UDM metadata / content). NOTE: logs are ordered by\n" +
			"resource name, not time, so this is a sample — pass --since to bound by time.\n" +
			"--json emits structured records (use it for logs with embedded newlines).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			if logType == "" {
				return fmt.Errorf("a LOG_TYPE is required (e.g. KONG_GATEWAY, NGINX, WINDOWS)")
			}
			var filter string
			if since > 0 {
				filter = fmt.Sprintf("collectionTime.seconds >= %d", time.Now().Add(-since).Unix())
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			lines, err := c.FetchSampleLogLines(baseContext(), logType, limit, filter)
			if err != nil {
				return err
			}
			if len(lines) == 0 {
				fmt.Fprintf(os.Stderr, "no logs for %q — widen --since or check the log type with `parsers versions %s`\n", logType, logType)
				return nil
			}
			return emitRawLines(lines)
		},
	}
	f := cmd.Flags()
	f.IntVar(&limit, "limit", 100, "max sample logs to fetch")
	f.DurationVar(&since, "since", 0, "only logs collected within this window (e.g. 2h); default: most recent")
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
