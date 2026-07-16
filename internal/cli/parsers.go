package cli

import (
	"encoding/base64"
	"encoding/json"
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

func newParsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parsers <verb>",
		Short: "Manage log parsers (versions, run, activate, extensions)",
		Long: "Operate parser versions directly:\n" +
			"  sample-logs  fetch a sample of RAW logs for a log type (to develop against)\n" +
			"  versions     list a log type's parser versions (id, state, validation, version)\n" +
			"  run          validate a CBN parser against sample logs (no server change)\n" +
			"  validate     show parsing errors from a submitted parser's validation report\n" +
			"  activate     make a parser version ACTIVE (guarded; auto-selects latest INACTIVE)\n" +
			"  deactivate   make a custom parser INACTIVE (revert to prebuilt)\n" +
			"  upgrade      preview + activate a prebuilt parser update (release candidate)\n" +
			"  rollback     roll back to the last used parser version\n" +
			"  delete       delete a specific parser version (guarded; --force for ACTIVE)\n" +
			"  extension    manage parser extensions (extract / setting / create / activate)\n\n" +
			"Parser-dev loop: sample-logs → write CBN → run → `push parsers` (submit) →\n" +
			"validate (why a submit's FAILED_PRECONDITION failed). Config-as-code is\n" +
			"`push parsers` (edit + create-new-version + activate).",
	}
	cmd.AddCommand(newParsersVersionsCmd(), newParsersRunCmd(), newParsersActivateCmd(),
		newParsersDeactivateCmd(), newParsersSampleLogsCmd(), newParsersValidateCmd(),
		newParsersDeleteCmd(), newParsersExtensionCmd(), newParsersUpgradeCmd(),
		newParsersRollbackCmd())
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
				ParserID        string `json:"parser_id"`
				State           string `json:"state"`
				Type            string `json:"type,omitempty"`
				ValidationStage string `json:"validation_stage,omitempty"`
				Version         string `json:"version,omitempty"`
				ReleaseStage    string `json:"release_stage,omitempty"`
				CreateTime      string `json:"create_time,omitempty"`
			}
			rows := make([]row, 0, len(ps))
			for i := range ps {
				r := row{
					ParserID:        parserID(ps[i].Name),
					State:           ps[i].State,
					Type:            ps[i].Type,
					ValidationStage: ps[i].ValidationStage,
					ReleaseStage:    ps[i].ReleaseStage,
					CreateTime:      ps[i].CreateTime,
				}
				if v, ok := ps[i].VersionInfo["version"]; ok {
					r.Version = fmt.Sprint(v)
				}
				rows = append(rows, r)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].CreateTime > rows[j].CreateTime })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PARSER ID\tSTATE\tTYPE\tVALIDATION\tVERSION\tCREATED")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					r.ParserID, r.State, r.Type,
					dashIfEmpty(r.ValidationStage), dashIfEmpty(r.Version),
					r.CreateTime)
			}
			return tw.Flush()
		},
	}
	return markJSON(cmd)
}

func newParsersRunCmd() *cobra.Command {
	var cbnFile, logsFile string
	var statedump bool
	cmd := &cobra.Command{
		Use:   "run <log-type> --cbn <file> --logs <file>",
		Short: "Validate a CBN parser against sample logs (no server change)",
		Long: "Run a parser's CBN against sample log lines and print the parsed UDM.\n" +
			"Purely inert — it creates and activates nothing — so it's safe to run before\n" +
			"`push parsers` (which would create a new version and activate it).\n\n" +
			"Per-log errors (UDM validation failures, field-type mismatches) are surfaced in\n" +
			"table mode as a one-line error per failed log; --json includes the full error\n" +
			"detail per result.\n\n" +
			"When a log produces no output, the statedump diagnostic (@onErrorCount and\n" +
			"intermediate state) is shown automatically. Use --statedump to see the full\n" +
			"statedump for every log (including successful ones).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(cbnFile)
			if err != nil {
				return fmt.Errorf("read --cbn: %w", err)
			}
			code := string(raw)
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
			resp, err := c.RunParser(baseContext(), args[0], code, logs)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			return printRunParserResults(resp, statedump)
		},
	}
	cmd.Flags().StringVar(&cbnFile, "cbn", "", "parser source (CBN) file to test (required)")
	cmd.Flags().StringVar(&logsFile, "logs", "", "sample log lines, one per line ('-' for stdin) (required)")
	cmd.Flags().BoolVar(&statedump, "statedump", false, "show full statedump for every log (debugging)")
	_ = cmd.MarkFlagRequired("cbn")
	_ = cmd.MarkFlagRequired("logs")
	return markJSON(cmd)
}

func printRunParserResults(resp *chronicle.RunParserResponse, showStatedump bool) error {
	if len(resp.RunParserResults) == 0 {
		fmt.Fprintln(os.Stdout, "no results.")
		return nil
	}
	ok, errCount := 0, 0
	for i, r := range resp.RunParserResults {
		switch {
		case r.Error != nil:
			errCount++
			fmt.Fprintf(os.Stderr, "  [log %d] error: %s\n", i+1, strings.TrimSpace(r.Error.Message))
			printParsedFields(i+1, r)
			printStatedumpSummary(i+1, r, showStatedump)
		case r.ParsedEvents != nil && len(r.ParsedEvents.Events) > 0:
			ok++
			fmt.Fprintf(os.Stdout, "  [log %d] %d UDM event(s)\n", i+1, len(r.ParsedEvents.Events))
			if showStatedump {
				printStatedumpSummary(i+1, r, true)
			}
		default:
			errCount++
			fmt.Fprintf(os.Stderr, "  [log %d] no UDM output (parser produced no events)\n", i+1)
			printParsedFields(i+1, r)
			printStatedumpSummary(i+1, r, true)
		}
	}
	fmt.Fprintf(os.Stdout, "\n%d log(s): %d parsed, %d error(s). Use --json for the full UDM output.\n",
		len(resp.RunParserResults), ok, errCount)
	if errCount > 0 {
		return fmt.Errorf("%d log(s) failed to parse", errCount)
	}
	return nil
}

func printParsedFields(logNum int, r chronicle.RunParserResult) {
	if len(r.FailedFieldsAndErrors) > 0 {
		var fields map[string]any
		if json.Unmarshal(r.FailedFieldsAndErrors, &fields) == nil && len(fields) > 0 {
			fmt.Fprintf(os.Stderr, "  [log %d] failed fields:\n", logNum)
			for k, v := range fields {
				fmt.Fprintf(os.Stderr, "           %s: %v\n", k, v)
			}
		}
	}
	if len(r.ParsedFields) > 0 {
		var fields map[string]any
		if json.Unmarshal(r.ParsedFields, &fields) == nil && len(fields) > 0 {
			fmt.Fprintf(os.Stderr, "  [log %d] parsed fields (partial):\n", logNum)
			for k, v := range fields {
				fmt.Fprintf(os.Stderr, "           %s: %v\n", k, v)
			}
		}
	}
}

// printStatedumpSummary extracts key diagnostics from the statedump results.
// When verbose is true, the full statedump text is printed; otherwise only a
// one-line summary (@onErrorCount + @output size) is shown.
func printStatedumpSummary(logNum int, r chronicle.RunParserResult, verbose bool) {
	if len(r.StatedumpResults) == 0 {
		return
	}
	for _, raw := range r.StatedumpResults {
		var entry struct {
			Result string `json:"statedumpResult"`
		}
		if json.Unmarshal(raw, &entry) != nil || entry.Result == "" {
			continue
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "  [log %d] statedump:%s\n", logNum, entry.Result)
		} else {
			printStatedumpOneLiner(logNum, entry.Result)
		}
	}
}

func printStatedumpOneLiner(logNum int, dump string) {
	var state map[string]any
	start := strings.Index(dump, "{")
	if start < 0 {
		return
	}
	if json.Unmarshal([]byte(dump[start:]), &state) != nil {
		return
	}
	var parts []string
	if v, ok := state["@onErrorCount"]; ok {
		if n, ok := v.(float64); ok && n > 0 {
			parts = append(parts, fmt.Sprintf("on_error fired %d time(s)", int(n)))
		}
	}
	if v, ok := state["@output"]; ok {
		if arr, ok := v.([]any); ok && len(arr) == 0 {
			parts = append(parts, "no @output merge")
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(os.Stderr, "  [log %d] statedump: %s\n", logNum, strings.Join(parts, "; "))
	}
}

func newParsersActivateCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "activate <log-type> [parser-id]",
		Short: "Make a parser version ACTIVE (guarded; live ingestion switches)",
		Long: "Activate a specific parser version for a log type — live ingestion switches to\n" +
			"it immediately. Guarded: dry-run by default, --yes to apply.\n\n" +
			"With one argument (log-type only), the latest INACTIVE CUSTOM parser is auto-\n" +
			"selected — the typical flow after `push parsers` creates a new version. Use\n" +
			"`parsers versions` to find a prior version's id to roll back to.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := args[0]
			var pid string
			if len(args) == 2 {
				pid = args[1]
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if pid == "" {
				resolved, rerr := resolveLatestInactiveCustom(c, logType)
				if rerr != nil {
					return rerr
				}
				pid = resolved
			}
			target := fmt.Sprintf("activate parser %s/%s", logType, pid)
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				return c.ActivateParser(baseContext(), logType, pid)
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// resolveLatestInactiveCustom finds the most recently created INACTIVE CUSTOM
// parser for a log type — the version a user just created and wants to activate.
func resolveLatestInactiveCustom(c *chronicle.Client, logType string) (string, error) {
	ps, err := c.ListParsers(baseContext(), logType)
	if err != nil {
		return "", err
	}
	var best *chronicle.Parser
	for i := range ps {
		if ps[i].State != "INACTIVE" || ps[i].Type != "CUSTOM" {
			continue
		}
		if best == nil || ps[i].CreateTime > best.CreateTime {
			best = &ps[i]
		}
	}
	if best == nil {
		return "", fmt.Errorf("no INACTIVE CUSTOM parser for %q — specify the parser-id explicitly", logType)
	}
	pid := parserID(best.Name)
	fmt.Fprintf(os.Stderr, "auto-selected parser %s (latest INACTIVE CUSTOM, created %s)\n", pid, best.CreateTime)
	return pid, nil
}
