package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// runUDMQuery runs a UDM event search and emits the result — the shared core of
// `query udm`, `query run`, and `query saved <name>`. limitChanged reports
// whether the operator set --limit explicitly (so --raw can apply its smaller
// default only when they did not). A window wider than searchWindowCap is
// searched in sequential chunks and the results merged (see query_window.go).
func runUDMQuery(filter string, q queryWindowFlags, limitChanged bool) error {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return fmt.Errorf("empty UDM query")
	}
	// Reject a non-positive look-back before any work (only meaningful when
	// --from is not set; an explicit window comes from --from/--to).
	if q.fromTS == "" {
		if err := checkHours(q.hours); err != nil {
			return err
		}
	}
	if q.meta && q.out == "" {
		return fmt.Errorf("--meta needs --out (the sidecar describes the saved file)")
	}
	limit := q.limit
	// --raw fetches a raw log per matched event, so cap conservatively unless the
	// operator set --limit explicitly (the event-only default is 10000).
	if q.raw && !limitChanged {
		limit = 100
	}

	start, end, err := resolveWindow(q.hours, q.fromTS, q.toTS)
	if err != nil {
		return err
	}
	chunks := chunkWindow(start, end, searchWindowCap)
	announceChunks(chunks)

	// Bulk fetches stream the whole result in one request per chunk; give them a
	// wider default deadline than the 60s general --timeout (explicit wins).
	timeout := requestTimeout
	if q.all || q.raw || q.countOnly {
		timeout = effectiveSearchTimeout()
	}
	c, err := newChronicleClientTimeout(timeout)
	if err != nil {
		return err
	}
	ctx := baseContext()

	if q.countOnly {
		_, counts, total, err := fetchEventsComplete(ctx, c, filter, chunks, 0)
		if err != nil {
			return err
		}
		return printCountOnly(total, counts)
	}
	if q.raw {
		return runUDMQueryRaw(ctx, c, filter, chunks, limit)
	}

	var (
		events []json.RawMessage
		counts []chunkCount
		total  *int
	)
	if q.all {
		// Complete-results engine: the full set plus the headline match count.
		maxEvents := limit
		if !limitChanged {
			maxEvents = 10000
		}
		evs, cc, t, err := fetchEventsComplete(ctx, c, filter, chunks, maxEvents)
		if err != nil {
			return err
		}
		events, counts, total = evs, cc, &t
		if t > len(events) {
			fmt.Fprintf(os.Stderr, "note: %d total match(es); returned %d — raise --limit or narrow the window for more.\n",
				t, len(events))
		}
	} else {
		evs, cc, truncated, err := fetchEventsPaged(ctx, c, filter, chunks, limit)
		if err != nil {
			return err
		}
		events, counts = evs, cc
		// Warn (to stderr, so piped output stays clean) when more matched than --limit.
		if truncated {
			fmt.Fprintf(os.Stderr, "warning: result truncated at --limit=%d; more events match — raise --limit, narrow the window, or use --all.\n", limit)
		}
	}
	if err := renderEvents(events, q.output()); err != nil {
		return err
	}
	if q.meta {
		return writeMetaSidecar(q.out, buildEvidenceMeta(filter, start, end, len(events), counts, total))
	}
	return nil
}

// runUDMQueryRaw downloads each matched event's FULL raw log and prints one per
// line (for `parsers run --logs -`). Lifts each raw-log id (udm.metadata.id)
// and fetches the complete bytes, with stderr progress on large sets.
func runUDMQueryRaw(ctx context.Context, c *chronicle.Client, filter string, chunks []searchWindow, limit int) error {
	events, _, truncated, err := fetchEventsPaged(ctx, c, filter, chunks, limit)
	if err != nil {
		return err
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "warning: result truncated at --limit=%d; more events match — raise --limit or narrow the time range.\n", limit)
	}
	ids := chronicle.RawLogIDsFromUDMEvents(events)
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "no raw logs: the matched events carry no raw-log id (or none matched)")
		return nil
	}
	lines, err := fetchRawLinesProgress(ctx, c, ids)
	if err != nil {
		return err
	}
	return emitRawLines(lines)
}

// readQueryText loads a UDM predicate from a file path, or from stdin when path
// is "-". Surrounding whitespace and a trailing newline are trimmed. `#`-prefixed
// and blank lines are dropped so a query file can carry comments.
func readQueryText(path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	var lines []string
	for ln := range strings.SplitSeq(string(data), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		lines = append(lines, t)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// queryWindowFlags are the shared time-window/limit/raw + output flags of a UDM run.
type queryWindowFlags struct {
	hours     int
	fromTS    string
	toTS      string
	limit     int
	raw       bool
	all       bool
	countOnly bool
	meta      bool
	enrichIP  bool
	format    string
	fields    string
	out       string
}

func (q *queryWindowFlags) bind(f *cobra.Command) {
	fl := f.Flags()
	fl.IntVar(&q.hours, "hours", 24, "look-back window in hours when --from is not given")
	fl.StringVar(&q.fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	fl.StringVar(&q.toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	fl.IntVar(&q.limit, "limit", 10000, "maximum number of events to return")
	fl.BoolVar(&q.raw, "raw", false, "print each matched event's FULL raw log line instead of the event summary")
	fl.BoolVar(&q.all, "all", false, "return the complete result set via the search-view engine "+
		"(reports the total match count; per-request deadline defaults to 10m unless --timeout is set)")
	fl.BoolVar(&q.countOnly, "count-only", false, "print only the TOTAL match count, no event data "+
		"(complete-results engine; far cheaper than fetching events to count them)")
	fl.BoolVar(&q.meta, "meta", false, "with --out: also write a <file>.meta.json sidecar recording the "+
		"query, window, counts, save time, and tool version (evidence provenance)")
	fl.BoolVar(&q.enrichIP, "enrich-ip", false, "append IP geolocation columns (country, state, ASN, carrier) "+
		"to the --fields projection — works with table, csv, and jsonl output")
	fl.StringVar(&q.format, "format", "", "output format: table|json|jsonl|csv (default: table on a terminal, jsonl when piped)")
	fl.StringVar(&q.fields, "fields", "", "comma-separated UDM field paths to project (e.g. metadata.event_type,principal.hostname)")
	fl.StringVar(&q.out, "out", "", "write results to a file instead of stdout")
	f.MarkFlagsMutuallyExclusive("count-only", "raw")
	f.MarkFlagsMutuallyExclusive("count-only", "all")
	f.MarkFlagsMutuallyExclusive("count-only", "enrich-ip")
}

// ipGeoFields are the UDM field paths appended by --enrich-ip: the inline
// principal location (country, state) plus the ipGeoArtifact enrichment (ASN,
// carrier). extractUDMField auto-enters singleton arrays, so the dotted path
// through ipGeoArtifact resolves without an explicit [0].
var ipGeoFields = []string{
	"principal.ip",
	"principal.location.countryOrRegion",
	"principal.location.state",
	"principal.ipGeoArtifact.network.asn",
	"principal.ipGeoArtifact.network.carrierName",
}

// output builds the result-rendering choice from the flags.
func (q *queryWindowFlags) output() resultOutput {
	fields := splitFields(q.fields)
	if q.enrichIP {
		fields = append(fields, ipGeoFields...)
	}
	return resultOutput{format: q.format, fields: fields, out: q.out}
}

func init() {
	queryCmd, _, err := rootCmd.Find([]string{"search"})
	if err != nil || queryCmd == nil {
		return // search is registered in query.go init; defensively no-op
	}
	queryCmd.AddCommand(newQueryRunCmd(), newQuerySavedCmd(), newQueryStatsCmd())
}

// newQueryRunCmd runs a UDM predicate read from a file or stdin — so a
// version-controlled `.udm` file is a runnable query, not just shell history.
func newQueryRunCmd() *cobra.Command {
	var (
		w    queryWindowFlags
		file string
	)
	cmd := &cobra.Command{
		Use:   "run (--file <path> | --file -)",
		Short: "Run a UDM query loaded from a file or stdin (`-`)",
		Long: "Run a UDM event search whose predicate is read from a file (--file <path>)\n" +
			"or stdin (--file -), so a tracked .udm file is a runnable, reviewable query.\n" +
			"Blank and #-comment lines in the file are ignored. Same window/--limit/--raw\n" +
			"semantics as `query udm`.",
		Example: "  secopsctl search run --file detections/failed-logins.udm --hours 24\n" +
			"  echo 'metadata.event_type = \"NETWORK_CONNECTION\"' | secopsctl search run --file -",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required (a path, or - for stdin)")
			}
			filter, err := readQueryText(file)
			if err != nil {
				return fmt.Errorf("read query --file %q: %w", file, err)
			}
			return runUDMQuery(filter, w, cmd.Flags().Changed("limit"))
		},
	}
	w.bind(cmd)
	cmd.Flags().StringVar(&file, "file", "", "path to a UDM query file, or - to read from stdin")
	return markJSON(cmd)
}

// The `query saved` command is server-side (chronicle users/me/searchQueries) and
// lives in query_saved_server.go.
