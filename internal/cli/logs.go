package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newLogsCmd())
}

// newLogsCmd groups read-only access to raw (unparsed) ingested logs.
func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read raw (unparsed) ingested logs (read-only)",
	}
	cmd.AddCommand(newLogsRawCmd())
	return cmd
}

func newLogsRawCmd() *cobra.Command {
	var (
		since    time.Duration
		limit    int
		query    string
		unparsed bool
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "raw <LOG_TYPE>",
		Short: "Fetch recent FULL raw log lines for a log type (pipe into `parsers run --logs -`)",
		Long: "Download recent complete raw (unparsed) log lines for a log type — the exact\n" +
			"bytes the platform ingested — to develop or validate a parser. The lines print\n" +
			"one per line, so they pipe straight into a parser test:\n\n" +
			"  secopsctl logs raw KONG_GATEWAY --since 2h --limit 50 | \\\n" +
			"    secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -\n\n" +
			"--unparsed restricts to logs that AREN'T being normalized (the ones a broken\n" +
			"parser is failing on). Reads only; --json emits structured records (use it for\n" +
			"logs with embedded newlines, which can't be one-per-line).\n\n" +
			"Mechanics: runs :searchRawLogs to find matching raw-log ids, then\n" +
			"legacyFindRawLogs to download the COMPLETE bytes (the search match itself only\n" +
			"carries an 80-char preview).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			if logType == "" {
				return fmt.Errorf("a LOG_TYPE is required (e.g. KONG_GATEWAY, NGINX, WINDOWS)")
			}
			q := strings.TrimSpace(query)
			if q == "" {
				q = `raw = /.*/` // match all logs of the type
			}
			if unparsed {
				q += " parsed = false"
			}
			if since <= 0 {
				return fmt.Errorf("--since must be positive (e.g. 30m, 2h, 24h)")
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			end := time.Now().UTC()
			start := end.Add(-since)
			lines, err := c.FetchRawLogLines(baseContext(), q, []string{logType}, start, end, limit)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(lines)
			}
			if len(lines) == 0 {
				hint := "widen --since or check the log type with `secopsctl pull parsers`"
				if unparsed {
					hint = "the log type may be parsing cleanly — drop --unparsed to include all logs"
				}
				fmt.Fprintf(os.Stderr, "no raw logs for %q in the last %s — %s\n", logType, since, hint)
				return nil
			}
			multiline := 0
			for _, l := range lines {
				// One raw log per line, emitted verbatim (trailing newline trimmed) so it
				// round-trips into `parsers run --logs -`.
				text := strings.TrimRight(l.Text, "\r\n")
				if strings.Contains(text, "\n") {
					multiline++ // an interior newline — this log spans multiple output lines
				}
				fmt.Fprintln(os.Stdout, text)
			}
			// `parsers run --logs -` reads one log per line, so a raw log with embedded
			// newlines (a stack trace, a pretty-printed JSON body) would be split into
			// fragments. Warn rather than corrupt silently; --json keeps each log intact.
			if multiline > 0 {
				fmt.Fprintf(os.Stderr, "warning: %d raw log(s) contain embedded newlines and span "+
					"multiple lines — `parsers run --logs -` treats each line as a separate log; "+
					"use --json for those\n", multiline)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.DurationVar(&since, "since", time.Hour, "look-back window (e.g. 30m, 2h, 24h)")
	f.IntVar(&limit, "limit", 100, "max raw lines to fetch")
	f.StringVar(&query, "query", "", "raw-log search expression (default: `raw = /.*/`, i.e. all logs of the type)")
	f.BoolVar(&unparsed, "unparsed", false, "restrict to logs that aren't being normalized (the parser-dev case)")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return cmd
}
