package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// isAggregationQuery reports whether a UDM query contains a match: or outcome:
// section header, which makes it an aggregation query for the stats engine (the
// plain event-search engine rejects or empty-answers it). Section headers are
// valid mid-line too (single-line stats queries), so the check strips quoted
// string contents first, then looks for a token preceded by whitespace or the
// start of the query.
func isAggregationQuery(query string) bool {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case inStr && c == '\\' && i+1 < len(query):
			i++ // skip the escaped character inside a string
		case c == '"':
			inStr = !inStr
			b.WriteByte(' ')
		case inStr:
			// drop string contents
		default:
			b.WriteByte(c)
		}
	}
	stripped := b.String()
	for _, section := range []string{"match:", "outcome:"} {
		for idx := strings.Index(stripped, section); idx >= 0; {
			if idx == 0 || stripped[idx-1] == ' ' || stripped[idx-1] == '\t' || stripped[idx-1] == '\n' {
				return true
			}
			rest := strings.Index(stripped[idx+len(section):], section)
			if rest < 0 {
				break
			}
			idx += len(section) + rest
		}
	}
	return false
}

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
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search the SIEM (read-only): udm events, raw logs, stats, saved searches",
		Long: "Search Google SecOps (read-only). Subcommands:\n" +
			"  udm       point-in-time UDM event search over a time window\n" +
			"  raw       content-based raw-log search (regex over the raw bytes)\n" +
			"  stats     aggregation (stats) query\n" +
			"  event     drill into one event (enriched UDM / unenriched UDM / raw log)\n" +
			"  export    export ALL matching events to CSV (server-side; not capped)\n" +
			"  validate  check a query's syntax without running it\n" +
			"  saved     server-side saved & shared searches\n" +
			"  run       run a UDM query loaded from a file or stdin\n\n" +
			"Natural-language (Gemini) search lives under `secopsctl gemini`.",
	}

	var w queryWindowFlags

	udmCmd := &cobra.Command{
		Use:   "udm <filter>",
		Short: "Run a UDM event search over a time window",
		Long: "Run a UDM event search over [start, end]. The window defaults to the last\n" +
			"--hours; --from/--to (RFC3339 / ISO-8601) override it. A window wider than the\n" +
			"90-day API cap is searched automatically in sequential ≤90-day chunks and the\n" +
			"results merged (per-chunk counts on stderr) — a year-long window just works.\n\n" +
			"--raw prints each matched event's FULL raw (ingested) log line instead of the\n" +
			"event summary — one per line, to pipe into a parser test. This is how to pull\n" +
			"the raw logs for a log type whose parser is missing/broken (they normalize to\n" +
			"GENERIC_EVENT):\n\n" +
			"  secopsctl search udm 'metadata.log_type = \"KONG_GATEWAY\" AND \\\n" +
			"      metadata.event_type = \"GENERIC_EVENT\"' --raw --limit 50 | \\\n" +
			"    secopsctl parsers run KONG_GATEWAY --cbn parser.conf --logs -\n\n" +
			"With --raw, --limit defaults to 100 (one raw fetch per matched event).\n\n" +
			"--count-only answers \"how many events match?\" without downloading them;\n" +
			"--out + --meta save results with a .meta.json provenance sidecar (query,\n" +
			"window, counts, tool version) for evidence trails.\n\n" +
			"--enrich-ip appends IP geolocation columns (country, state, ASN, carrier)\n" +
			"to the field projection — combine with --fields for a login-audit table.",
		Example: "  # network connections in the last 6 hours\n" +
			"  secopsctl search udm 'metadata.event_type = \"NETWORK_CONNECTION\"' --hours 6\n\n" +
			"  # a fixed window, machine-readable\n" +
			"  secopsctl search udm 'principal.hostname = \"host-01\"' \\\n" +
			"      --from 2024-01-02T00:00:00Z --to 2024-01-03T00:00:00Z --json\n\n" +
			"  # a year-long window: auto-chunked; count first, then save with provenance\n" +
			"  secopsctl search udm '<filter>' --from 2025-01-01 --to 2026-01-01 --count-only\n" +
			"  secopsctl search udm '<filter>' --from 2025-01-01 --to 2026-01-01 --all \\\n" +
			"      --format jsonl --out evidence/events.jsonl --meta\n\n" +
			"  # login audit with IP geo enrichment\n" +
			"  secopsctl search udm 'metadata.event_type = \"USER_LOGIN\"' --hours 72 \\\n" +
			"      --fields principal.user.userid,metadata.event_timestamp --enrich-ip --format csv",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if isAggregationQuery(query) {
				fmt.Fprintln(os.Stderr, "note: aggregation query (match:/outcome:) — routing to `search stats`.")
				return runStatsFromUDM(query, w.hours, w.fromTS, w.toTS)
			}
			return runUDMQuery(args[0], w, cmd.Flags().Changed("limit"))
		},
	}
	w.bind(udmCmd)

	searchCmd.AddCommand(markJSON(udmCmd), newQueryRawCmd(),
		newGeminiGenerateQueryCmd())
	rootCmd.AddCommand(searchCmd)
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
			"  secopsctl search raw 'GET /healthz' --limit 50 | \\\n" +
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
		Event    json.RawMessage `json:"event"`
		Metadata *udmMetadata    `json:"metadata"`
	}
	if err := json.Unmarshal(ev, &outer); err != nil {
		return when, etype
	}

	// The UDM lives under "udm" (:udmSearch) or "event" (search-view UdmEventInfo);
	// some shapes inline metadata at the top level.
	meta := outer.Metadata
	root := outer.UDM
	if len(root) == 0 {
		root = outer.Event
	}
	if len(root) > 0 {
		var inner struct {
			Metadata *udmMetadata `json:"metadata"`
		}
		if err := json.Unmarshal(root, &inner); err == nil && inner.Metadata != nil {
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
