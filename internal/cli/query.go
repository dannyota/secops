package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
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

func init() {
	queryCmd := &cobra.Command{
		Use:   "query",
		Short: "Run read-only queries against the tenant (udm, nl)",
		Long: "Query the tenant (read-only). Two kinds:\n" +
			"  udm  point-in-time UDM event search over a time window\n" +
			"  nl   natural-language search — describe what you want; it translates to UDM\n" +
			"       (`--translate-only` prints the UDM without running it).",
	}

	var (
		hours  int
		fromTS string
		toTS   string
		limit  int
	)

	udmCmd := &cobra.Command{
		Use:   "udm <filter>",
		Short: "Run a UDM event search over a time window",
		Long: "Run a UDM event search over [start, end]. The window defaults to the last\n" +
			"--hours; --from/--to (RFC3339 / ISO-8601) override it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := args[0]

			// Resolve the time range (ports query.py): end = --to or now UTC;
			// start = --from or end-hours.
			var (
				end time.Time
				err error
			)
			if toTS != "" {
				if end, err = parseQueryTS(toTS); err != nil {
					return err
				}
			} else {
				end = time.Now().UTC()
			}

			var start time.Time
			if fromTS != "" {
				if start, err = parseQueryTS(fromTS); err != nil {
					return err
				}
			} else {
				start = end.Add(-time.Duration(hours) * time.Hour)
			}

			if !start.Before(end) {
				return fmt.Errorf("start time (%s) must be before end time (%s)",
					start.Format(time.RFC3339), end.Format(time.RFC3339))
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

	queryCmd.AddCommand(udmCmd, newQueryNLCmd())
	rootCmd.AddCommand(queryCmd)
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
	return cmd
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
