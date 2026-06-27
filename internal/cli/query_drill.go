package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// query_drill.go adds the per-result agent surfaces: export a full result set to
// CSV (server-side), drill into one event (enriched UDM / UDM / raw log), and
// validate a query without running it.

func init() {
	queryCmd, _, err := rootCmd.Find([]string{"search"})
	if err != nil || queryCmd == nil {
		return // search is registered in query.go init; defensively no-op
	}
	queryCmd.AddCommand(newQueryExportCmd(), newQueryEventCmd(), newQueryValidateCmd())
}

// newQueryExportCmd exports ALL matching events of a UDM query to CSV server-side
// (legacyFetchUdmSearchCsv) — the bulk path that is not capped at --limit like
// `query udm`. Columns are UI labels the server maps to UDM fields.
func newQueryExportCmd() *cobra.Command {
	var (
		hours     int
		fromTS    string
		toTS      string
		fieldsCSV string
		out       string
		caseSens  bool
	)
	cmd := &cobra.Command{
		Use:   "export <filter>",
		Short: "Export ALL matching events to CSV (server-side; not capped at --limit)",
		Long: "Export every event matching a UDM query over [start, end) to CSV, projected\n" +
			"onto the chosen columns. Unlike `query udm` (capped at --limit, client-side),\n" +
			"this is the server-side bulk export and returns the complete result set.\n\n" +
			"--fields are the console column labels (e.g. timestamp, user, hostname,\n" +
			"\"process name\", \"raw log\", or any udm.additional.* path); unsupported labels\n" +
			"are reported and skipped. --out writes to a file instead of stdout.",
		Example: "  secopsctl search export 'metadata.event_type = \"NETWORK_DNS\"' --hours 24 --out dns.csv\n" +
			"  secopsctl search export 'principal.hostname = \"host-01\"' --fields timestamp,user,hostname",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := strings.TrimSpace(args[0])
			if filter == "" {
				return fmt.Errorf("empty UDM query")
			}
			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}
			fields := splitFields(fieldsCSV)
			if len(fields) == 0 {
				fields = []string{"timestamp", "user", "hostname", "process name"}
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			res, err := c.ExportUDMSearchCSVResult(baseContext(), filter, start, end, fields, caseSens)
			if err != nil {
				return err
			}
			if len(res.InvalidFields) > 0 {
				fmt.Fprintf(os.Stderr, "warning: server rejected field(s): %s (omitted from the CSV)\n",
					strings.Join(res.InvalidFields, ", "))
			}
			if res.TooManyEvents {
				fmt.Fprintln(os.Stderr, "warning: result truncated server-side; narrow the time range for the complete set")
			}
			o := resultOutput{out: out}
			w, closeFn, err := o.writer()
			if err != nil {
				return err
			}
			defer func() { _ = closeFn() }()
			_, err = fmt.Fprintln(w, res.CSV)
			return err
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	f.StringVar(&fieldsCSV, "fields", "", "comma-separated column labels (default: timestamp,user,hostname,\"process name\")")
	f.StringVar(&out, "out", "", "write the CSV to a file instead of stdout")
	f.BoolVar(&caseSens, "case-sensitive", false, "case-sensitive matching (default: case-insensitive, like the console)")
	return markJSON(cmd)
}

// newQueryEventCmd drills into one event by id: the enriched UDM (default), the
// unenriched UDM (--udm), or the original raw log line(s) (--raw).
func newQueryEventCmd() *cobra.Command {
	var (
		raw     bool
		udmOnly bool
		token   bool
	)
	cmd := &cobra.Command{
		Use:   "event <id>",
		Short: "Inspect one event by id: enriched UDM (default), --udm, or --raw log",
		Long: "Fetch one event's detail by id (the base64 udm.metadata.id from a search\n" +
			"result). Default prints the ENRICHED UDM event (geo / threat-intel / entity\n" +
			"overlays). --udm prints the unenriched UDM event; --raw prints the original\n" +
			"raw log line(s). --token treats the argument as a search token instead of an id\n" +
			"(for --udm).",
		Example: "  secopsctl search event 'AAAA…=' --json\n" +
			"  secopsctl search event 'AAAA…=' --raw",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("an event id is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			switch {
			case raw:
				lines, err := c.FindRawLogLines(ctx, []string{id})
				if err != nil {
					return err
				}
				return emitRawLines(lines)
			case udmOnly:
				var ids, tokens []string
				if token {
					tokens = []string{id}
				} else {
					ids = []string{id}
				}
				res, err := c.FindUDMEvents(ctx, ids, tokens, true)
				if err != nil {
					return err
				}
				return renderEvents(res.UDMEvents(), resultOutput{format: formatJSON})
			default:
				ev, err := c.FetchEnrichedEvent(ctx, id, "")
				if err != nil {
					return err
				}
				if jsonOut {
					return emitJSON(ev)
				}
				return writeRawJSON(os.Stdout, ev.UDM)
			}
		},
	}
	f := cmd.Flags()
	f.BoolVar(&raw, "raw", false, "print the original raw log line(s) for the event")
	f.BoolVar(&udmOnly, "udm", false, "print the unenriched UDM event(s)")
	f.BoolVar(&token, "token", false, "treat the argument as a search token instead of an event id (with --udm)")
	cmd.MarkFlagsMutuallyExclusive("raw", "udm")
	return markJSON(cmd)
}

// newQueryValidateCmd validates a UDM query's syntax without running it — an
// agent guardrail before spending a search. Exits non-zero on an invalid query.
func newQueryValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <query>",
		Short: "Validate a UDM query's syntax without running it",
		Long: "Check a UDM query for syntax errors without running a search. Prints the\n" +
			"detected query type and (when invalid) the error message; exits non-zero on an\n" +
			"invalid query so a script/agent can gate on it.",
		Example: "  secopsctl search validate 'metadata.event_type = \"NETWORK_DNS\"'",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("a query is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			v, err := c.ValidateQuery(baseContext(), query)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(v)
			}
			if v.IsValid {
				fmt.Printf("valid  (%s)\n", v.QueryType)
				return nil
			}
			msg := v.ValidationMessage
			if msg == "" {
				msg = v.ErrorType
			}
			// Print the verdict, then a sentinel error for a non-zero exit.
			fmt.Printf("invalid: %s\n", msg)
			return fmt.Errorf("query is invalid")
		},
	}
	return markJSON(cmd)
}
