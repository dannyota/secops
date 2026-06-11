package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// parseQueryTS coerces an ISO-8601 / RFC3339 timestamp into a UTC time. A
// trailing "Z" (or "z") is honored by RFC3339; a value carrying no zone is
// assumed UTC. This mirrors query.py's _parse_ts.
func parseQueryTS(value string) (time.Time, error) {
	text := strings.TrimSpace(value)
	// RFC3339 with an explicit offset or Z.
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t.UTC(), nil
	}
	// Zone-less ISO-8601 (e.g. "2024-01-02T03:04:05"): assume UTC.
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.UTC); err == nil {
		return t.UTC(), nil
	}
	// Date-only (e.g. "2024-01-02"): assume midnight UTC.
	if t, err := time.ParseInLocation("2006-01-02", text, time.UTC); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q (want RFC3339 / ISO-8601)", value)
}

// resolveWindow resolves a [start, end) search window from --hours / --from / --to:
// end = --to or now UTC; start = --from or end-hours. Errors when start >= end.
func resolveWindow(hours int, fromTS, toTS string) (start, end time.Time, err error) {
	if toTS != "" {
		if end, err = parseQueryTS(toTS); err != nil {
			return start, end, err
		}
	} else {
		end = time.Now().UTC()
	}
	if fromTS != "" {
		if start, err = parseQueryTS(fromTS); err != nil {
			return start, end, err
		}
	} else {
		start = end.Add(-time.Duration(hours) * time.Hour)
	}
	if !start.Before(end) {
		return start, end, fmt.Errorf("start time (%s) must be before end time (%s)",
			start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	return start, end, nil
}

// emitRawLines prints raw log lines one per line (to pipe into `parsers run
// --logs -`), or the structured records under --json. It warns when a log carries
// embedded newlines, which the one-per-line consumer would split into fragments.
func emitRawLines(lines []chronicle.RawLogLine) error {
	if jsonOut {
		return emitJSON(lines)
	}
	multiline := 0
	for _, l := range lines {
		text := strings.TrimRight(l.Text, "\r\n")
		if strings.Contains(text, "\n") {
			multiline++
		}
		fmt.Fprintln(os.Stdout, text)
	}
	if multiline > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d raw log(s) contain embedded newlines and span "+
			"multiple lines — `parsers run --logs -` treats each line as a separate log; "+
			"use --json for those\n", multiline)
	}
	return nil
}

func init() {
	queryCmd := &cobra.Command{
		Use:   "query",
		Short: "Run read-only queries against the tenant (udm, nl, raw)",
		Long: "Query the tenant (read-only). Three kinds:\n" +
			"  udm  point-in-time UDM event search over a time window (`--raw` prints each\n" +
			"       matched event's full raw log line)\n" +
			"  nl   natural-language search — describe what you want; it translates to UDM\n" +
			"       (`--translate-only` prints the UDM without running it)\n" +
			"  raw  content-based raw-log search — print full raw log lines matching a regex\n" +
			"       (reaches logs with no parser; complements `udm --raw`).",
	}

	var (
		hours  int
		fromTS string
		toTS   string
		limit  int
		raw    bool
	)

	udmCmd := &cobra.Command{
		Use:   "udm <filter>",
		Short: "Run a UDM event search over a time window",
		Long: "Run a UDM event search over [start, end]. The window defaults to the last\n" +
			"--hours; --from/--to (RFC3339 / ISO-8601) override it.\n\n" +
			"--raw prints each matched event's FULL raw (ingested) log line instead of the\n" +
			"event summary — one per line, to pipe into a parser test. This is how to pull\n" +
			"the raw logs for a log type whose parser is missing/broken (they normalize to\n" +
			"GENERIC_EVENT):\n\n" +
			"  secopsctl query udm 'metadata.log_type = \"KONG_GATEWAY\" AND \\\n" +
			"      metadata.event_type = \"GENERIC_EVENT\"' --raw --limit 50 | \\\n" +
			"    secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -\n\n" +
			"With --raw, --limit defaults to 100 (one raw fetch per matched event).",
		Example: "  # network connections in the last 6 hours\n" +
			"  secopsctl query udm 'metadata.event_type = \"NETWORK_CONNECTION\"' --hours 6\n\n" +
			"  # a fixed window, machine-readable\n" +
			"  secopsctl query udm 'principal.hostname = \"host-01\"' \\\n" +
			"      --from 2024-01-02T00:00:00Z --to 2024-01-03T00:00:00Z --json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := args[0]
			// Reject a non-positive look-back before any work (only meaningful when
			// --from is not set; an explicit window comes from --from/--to).
			if fromTS == "" {
				if err := checkHours(hours); err != nil {
					return err
				}
			}
			// --raw fetches a raw log per matched event, so cap conservatively unless
			// the operator set --limit explicitly (the event-only default is 10000).
			if raw && !cmd.Flags().Changed("limit") {
				limit = 100
			}

			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}

			events, more, err := c.SearchUDMPage(baseContext(), filter, start, end, limit)
			if err != nil {
				return err
			}
			// The server caps results at --limit; warn (to stderr, so --json stays
			// clean for piping) when it had more so a partial set isn't mistaken
			// for the full match.
			if more {
				fmt.Fprintf(os.Stderr, "warning: result truncated at --limit=%d; more events match — raise --limit or narrow the time range.\n", limit)
			}

			// --raw: download each matched event's FULL raw log and print one per line
			// (for `parsers run --logs -`). Reuses the events already fetched: lift each
			// raw-log id (udm.metadata.id) and fetch the complete bytes.
			if raw {
				ids := chronicle.RawLogIDsFromUDMEvents(events)
				if len(ids) == 0 {
					fmt.Fprintln(os.Stderr, "no raw logs: the matched events carry no raw-log id (or none matched)")
					return nil
				}
				lines, err := c.FindRawLogLines(baseContext(), ids)
				if err != nil {
					return err
				}
				return emitRawLines(lines)
			}

			if jsonOut {
				// Print the raw events verbatim as an indented JSON array. An
				// empty result set is rendered as "[]".
				if len(events) == 0 {
					fmt.Println("[]")
					return nil
				}
				b, err := json.MarshalIndent(events, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}

			fmt.Printf("UDM search returned %d event(s).\n", len(events))
			for i, ev := range events {
				when, etype := udmSummary(ev)
				fmt.Printf("  %4d  %s  %s\n", i+1, when, etype)
			}
			return nil
		},
	}

	f := udmCmd.Flags()
	f.IntVar(&hours, "hours", 24,
		"look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "",
		"explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "",
		"explicit end time (RFC3339 / ISO-8601); default: now")
	f.IntVar(&limit, "limit", 10000,
		"maximum number of events to return")
	f.BoolVar(&raw, "raw", false,
		"print each matched event's FULL raw log line (for `parsers run --logs -`) instead of the event summary")

	queryCmd.AddCommand(markJSON(udmCmd), newQueryNLCmd(), newQueryRawCmd(), newQueryGeminiCmd())
	rootCmd.AddCommand(queryCmd)
}

// newQueryRawCmd is content-based raw-log search (searchRawLogs): match raw bytes by
// regex and print each match's FULL raw log line. Complements `query udm --raw`
// (which scopes by metadata.log_type and needs a UDM event) — this reaches even logs
// with no parser at all, scoped by a distinctive content pattern.
func newQueryRawCmd() *cobra.Command {
	var (
		hours    int
		fromTS   string
		toTS     string
		limit    int
		unparsed bool
	)
	cmd := &cobra.Command{
		Use:   "raw <pattern>",
		Short: "Content-based raw-log search: print FULL raw lines matching a regex",
		Long: "Search raw (ingested) logs by CONTENT (searchRawLogs): <pattern> is a regex\n" +
			"matched against the raw bytes — use a distinctive substring of the log type you\n" +
			"want (it matches ANY log containing the pattern). Prints each match's FULL raw\n" +
			"log line, one per line, for `parsers run --logs -`:\n\n" +
			"  secopsctl query raw 'GET /healthz' --limit 50 | \\\n" +
			"    secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -\n\n" +
			"Unlike `query udm --raw` (scopes by metadata.log_type; needs a UDM event), this\n" +
			"reaches even logs with no parser at all. --unparsed adds `parsed = false`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := strings.TrimSpace(args[0])
			if pattern == "" {
				return fmt.Errorf("a search pattern is required (a regex matched against the raw bytes)")
			}
			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}
			// Escape the regex delimiter so a `/` in the pattern (e.g. a URL path like
			// `GET /healthz`) doesn't terminate the `/…/` literal early.
			q := "raw = /" + strings.ReplaceAll(pattern, "/", `\/`) + "/"
			if unparsed {
				q += " parsed = false" // searchRawLogs joins predicates by space, not AND
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			page, err := c.SearchRawLogsPage(ctx, q, nil, start, end, limit, 0, false, "", "")
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(page.RawLogs))
			for _, m := range page.RawLogs {
				if m.ID != "" {
					ids = append(ids, m.ID)
				}
				if limit > 0 && len(ids) >= limit { // pageSize is best-effort; enforce --limit
					break
				}
			}
			if len(ids) == 0 {
				fmt.Fprintf(os.Stderr, "no raw logs match /%s/ in the window — try a different pattern or widen --hours\n", pattern)
				return nil
			}
			lines, err := c.FindRawLogLines(ctx, ids)
			if err != nil {
				return err
			}
			return emitRawLines(lines)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	f.IntVar(&limit, "limit", 100, "max raw lines to fetch")
	f.BoolVar(&unparsed, "unparsed", false, "restrict to truly-unparsed logs (parsed = false)")
	return markJSON(cmd)
}

// newQueryNLCmd translates a natural-language description to a UDM query and runs
// it (or just prints the generated UDM with --translate-only).
func newQueryNLCmd() *cobra.Command {
	var (
		nlHours       int
		nlLimit       int
		translateOnly bool
	)
	cmd := &cobra.Command{
		Use:   "nl <natural language query>",
		Short: "Translate a natural-language query to UDM and search",
		Long: "Translate a natural-language description to a UDM query and run it over the\n" +
			"last --hours. --translate-only prints the generated UDM without searching.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			if translateOnly {
				udm, terr := c.TranslateNLToUDM(ctx, text)
				if terr != nil {
					return terr
				}
				if jsonOut {
					return emitJSON(map[string]string{"udm": udm})
				}
				fmt.Println(udm)
				return nil
			}
			start, end := timeWindow(nlHours)
			events, serr := c.NLSearch(ctx, text, start, end, nlLimit)
			if serr != nil {
				return serr
			}
			if jsonOut {
				return emitJSON(events)
			}
			for i, e := range events {
				when, etype := udmSummary(e)
				fmt.Printf("%-4d %-26s %s\n", i, when, etype)
			}
			fmt.Printf("\n%d event(s)\n", len(events))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&nlHours, "hours", 24, "look-back window in hours")
	f.IntVar(&nlLimit, "limit", 1000, "maximum number of events to return")
	f.BoolVar(&translateOnly, "translate-only", false, "print the generated UDM query; do not search")
	return markJSON(cmd)
}

// udmMetadata captures UDM metadata in both serializations the API may emit:
// proto-JSON camelCase and the snake_case form. Go's JSON matching is
// case-insensitive but underscore-sensitive, so event_timestamp does NOT match
// the eventTimestamp tag — both must be declared and the non-empty one chosen.
type udmMetadata struct {
	EventTimestamp      string `json:"eventTimestamp"`
	EventTimestampSnake string `json:"event_timestamp"`
	EventType           string `json:"eventType"`
	EventTypeSnake      string `json:"event_type"`
}

func (m *udmMetadata) timestamp() string {
	return firstNonEmpty(m.EventTimestamp, m.EventTimestampSnake)
}
func (m *udmMetadata) eventType() string { return firstNonEmpty(m.EventType, m.EventTypeSnake) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// udmSummary extracts a (timestamp, eventType) pair from a raw UDM event for
// the text summary. The event shape is typically {"udm": {"metadata": {...}}};
// some result shapes inline the metadata at the top level, so both are probed.
// Both camelCase and snake_case metadata keys are honored (parity with the
// legacy tool). Missing fields render as "?".
func udmSummary(ev json.RawMessage) (when, etype string) {
	when, etype = "?", "?"

	var outer struct {
		UDM      json.RawMessage `json:"udm"`
		Metadata *udmMetadata    `json:"metadata"`
	}
	if err := json.Unmarshal(ev, &outer); err != nil {
		return when, etype
	}

	meta := outer.Metadata
	if len(outer.UDM) > 0 {
		var inner struct {
			Metadata *udmMetadata `json:"metadata"`
		}
		if err := json.Unmarshal(outer.UDM, &inner); err == nil && inner.Metadata != nil {
			meta = inner.Metadata
		}
	}
	if meta == nil {
		return when, etype
	}
	if ts := meta.timestamp(); ts != "" {
		when = ts
	}
	if et := meta.eventType(); et != "" {
		etype = et
	}
	return when, etype
}

// newQueryGeminiCmd asks SecOps Gemini a question — YARA-L authoring help, UDM
// field questions, environment-grounded answers (users/me/conversations). The
// account must be opted in once (--opt-in does it in-place).
func newQueryGeminiCmd() *cobra.Command {
	var optIn bool
	cmd := &cobra.Command{
		Use:   "gemini <question>",
		Short: "Ask SecOps Gemini a question (read-only; --opt-in once per account)",
		Long: "Ask SecOps Gemini a question — YARA-L authoring help, UDM field questions,\n" +
			"and environment-grounded answers. Read-only: it returns an answer, it makes\n" +
			"no changes. The account must be opted in to Gemini once; --opt-in performs\n" +
			"that one-time enablement and can be combined with a question in the same run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			if optIn {
				if err := c.OptInToGemini(ctx); err != nil {
					return err
				}
			}
			resp, err := c.QueryGemini(ctx, args[0], "")
			if err != nil {
				if errors.Is(err, chronicle.ErrGeminiOptInRequired) {
					return fmt.Errorf("this account has not opted in to Gemini — re-run with --opt-in once: %w", err)
				}
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, resp.Raw)
			}
			text := resp.TextContent()
			if text == "" {
				// Live replies often carry HTML blocks instead of TEXT — render
				// them as plain prose.
				var parts []string
				for _, b := range resp.HTMLBlocks() {
					parts = append(parts, htmlToText(b.Content))
				}
				text = strings.Join(parts, "\n\n")
			}
			if text != "" {
				fmt.Fprintln(os.Stdout, text)
			}
			for _, b := range resp.CodeBlocks() {
				fmt.Fprintf(os.Stdout, "\n```\n%s\n```\n", b.Content)
			}
			if len(resp.References) > 0 {
				fmt.Fprintf(os.Stdout, "\n(%d reference block(s) — full structure with --json.)\n", len(resp.References))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&optIn, "opt-in", false, "opt this account in to Gemini first (one-time)")
	return markJSON(cmd)
}

// htmlToText renders Gemini's HTML prose as readable terminal text: list items
// become bullets, block elements become line breaks, the remaining tags drop,
// and common entities decode.
func htmlToText(s string) string {
	s = regexp.MustCompile(`(?i)<li[^>]*>`).ReplaceAllString(s, "\n  - ")
	s = regexp.MustCompile(`(?i)</(p|ul|ol|div|h[1-6])>|<br[^>]*>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n"))
}
