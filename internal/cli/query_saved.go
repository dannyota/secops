package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror"
)

// runUDMQuery runs a UDM event search and emits the result — the shared core of
// `query udm`, `query run`, and `query saved <name>`. limitChanged reports
// whether the operator set --limit explicitly (so --raw can apply its smaller
// default only when they did not).
func runUDMQuery(filter string, hours int, fromTS, toTS string, limit int, raw, limitChanged bool) error {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return fmt.Errorf("empty UDM query")
	}
	// Reject a non-positive look-back before any work (only meaningful when
	// --from is not set; an explicit window comes from --from/--to).
	if fromTS == "" {
		if err := checkHours(hours); err != nil {
			return err
		}
	}
	// --raw fetches a raw log per matched event, so cap conservatively unless the
	// operator set --limit explicitly (the event-only default is 10000).
	if raw && !limitChanged {
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
	// The server caps results at --limit; warn (to stderr, so --json stays clean
	// for piping) when it had more so a partial set isn't mistaken for the full
	// match.
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
		// Print the raw events verbatim as an indented JSON array; an empty result
		// set is rendered as "[]".
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

// queryWindowFlags are the shared time-window/limit/raw flags of a UDM run.
type queryWindowFlags struct {
	hours  int
	fromTS string
	toTS   string
	limit  int
	raw    bool
}

func (q *queryWindowFlags) bind(f *cobra.Command) {
	fl := f.Flags()
	fl.IntVar(&q.hours, "hours", 24, "look-back window in hours when --from is not given")
	fl.StringVar(&q.fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	fl.StringVar(&q.toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	fl.IntVar(&q.limit, "limit", 10000, "maximum number of events to return")
	fl.BoolVar(&q.raw, "raw", false, "print each matched event's FULL raw log line instead of the event summary")
}

func init() {
	queryCmd, _, err := rootCmd.Find([]string{"query"})
	if err != nil || queryCmd == nil {
		return // query is registered in query.go's init; defensively no-op
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
		Example: "  secopsctl query run --file detections/failed-logins.udm --hours 24\n" +
			"  echo 'metadata.event_type = \"NETWORK_CONNECTION\"' | secopsctl query run --file -",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required (a path, or - for stdin)")
			}
			filter, err := readQueryText(file)
			if err != nil {
				return fmt.Errorf("read query --file %q: %w", file, err)
			}
			return runUDMQuery(filter, w.hours, w.fromTS, w.toTS, w.limit, w.raw, cmd.Flags().Changed("limit"))
		},
	}
	w.bind(cmd)
	cmd.Flags().StringVar(&file, "file", "", "path to a UDM query file, or - to read from stdin")
	return markJSON(cmd)
}

// newQuerySavedCmd runs a query from the tracked saved-query pack by name, or
// lists the pack when given no name.
func newQuerySavedCmd() *cobra.Command {
	var (
		w   queryWindowFlags
		out string
	)
	cmd := &cobra.Command{
		Use:   "saved [<name>]",
		Short: "Run a named query from the tracked saved-query pack (or list the pack)",
		Long: "Run a saved query by NAME from the version-controlled query pack at\n" +
			"<dataRoot>/" + mirror.DirSavedQueries + "/<name>.udm, or list the pack when given no\n" +
			"name. Author queries as plain .udm files (one predicate, #-comments allowed) and\n" +
			"commit them so the team shares one reviewed set. Same window/--limit/--raw\n" +
			"semantics as `query udm`.",
		Example: "  secopsctl query saved                 # list the pack\n" +
			"  secopsctl query saved failed-logins --hours 24",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := filepath.Join(mirror.DataRoot(out), mirror.DirSavedQueries)
			if len(args) == 0 {
				return listSavedQueries(dir)
			}
			name := args[0]
			if err := validSavedQueryName(name); err != nil {
				return err
			}
			path := filepath.Join(dir, name+".udm")
			filter, err := readQueryText(path)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no saved query %q in %s (run `query saved` to list)", name, dir)
				}
				return err
			}
			return runUDMQuery(filter, w.hours, w.fromTS, w.toTS, w.limit, w.raw, cmd.Flags().Changed("limit"))
		},
	}
	w.bind(cmd)
	cmd.Flags().StringVar(&out, "out", "", "data root holding the saved-query pack (default: cwd)")
	return markJSON(cmd)
}

// validSavedQueryName rejects a saved-query name that could escape the pack
// directory (path separators or a parent-dir reference), so `query saved <name>`
// can only read a .udm inside the pack.
func validSavedQueryName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid saved-query name %q (a plain name, no path separators)", name)
	}
	return nil
}

// listSavedQueries prints the names of the .udm files in the pack directory.
func listSavedQueries(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("no saved-query pack at %s — create one by committing .udm files there.\n", dir)
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".udm") {
			names = append(names, strings.TrimSuffix(e.Name(), ".udm"))
		}
	}
	sort.Strings(names)
	if jsonOut {
		return emitJSON(names)
	}
	if len(names) == 0 {
		fmt.Printf("no saved queries in %s\n", dir)
		return nil
	}
	fmt.Printf("%d saved quer(ies) in %s:\n", len(names), dir)
	for _, n := range names {
		fmt.Printf("  %s\n", n)
	}
	return nil
}
