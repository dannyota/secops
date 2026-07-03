package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

// query_results.go renders UDM search results for agents and humans: JSONL (one
// event per line — stream/grep friendly), JSON (full array), CSV, or an aligned
// table; with optional --fields projection (dotted UDM paths) and --out to a file.

// Output formats for search results.
const (
	formatTable = "table"
	formatJSON  = "json"
	formatJSONL = "jsonl"
	formatCSV   = "csv"
)

// resultOutput captures the --format / --fields / --out choices for a result set.
type resultOutput struct {
	format string   // "", table, json, jsonl, csv
	fields []string // dotted UDM paths to project; empty = default per format
	out    string   // file path, or "" for stdout
}

// resolveFormat picks the effective format: an explicit --format wins, then the
// global --output, then --json means json; else jsonl when stdout is piped
// (agent-friendly) and table on an interactive terminal.
func (o resultOutput) resolveFormat() string {
	if f := effectiveFormat(o.format); f != "" {
		return f
	}
	if stdoutIsTerminal() {
		return formatTable
	}
	return formatJSONL
}

// writer opens the --out file (caller closes) or returns stdout.
func (o resultOutput) writer() (w io.Writer, closeFn func() error, err error) {
	if o.out == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.Create(o.out)
	if err != nil {
		return nil, nil, fmt.Errorf("open --out %q: %w", o.out, err)
	}
	return f, f.Close, nil
}

// renderEvents writes the events in the chosen format. fieldsRequired is the CSV
// default column set used when --fields is not given.
func renderEvents(events []json.RawMessage, o resultOutput) error {
	w, closeFn, err := o.writer()
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()

	switch o.resolveFormat() {
	case formatJSON:
		return writeIndentedJSON(w, events)
	case formatJSONL:
		return renderJSONL(w, events, o.fields)
	case formatCSV:
		return renderCSV(w, events, o.fields)
	default: // table
		return renderTable(w, events, o.fields)
	}
}

func writeIndentedJSON(w io.Writer, events []json.RawMessage) error {
	if len(events) == 0 {
		_, err := fmt.Fprintln(w, "[]")
		return err
	}
	b, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// renderJSONL prints one event per line: the full compact event, or a projected
// {field: value} object when --fields is given.
func renderJSONL(w io.Writer, events []json.RawMessage, fields []string) error {
	enc := json.NewEncoder(w)
	for _, ev := range events {
		if len(fields) == 0 {
			if err := enc.Encode(ev); err != nil {
				return err
			}
			continue
		}
		row := make(map[string]string, len(fields))
		for _, f := range fields {
			row[f] = extractUDMField(ev, f)
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func renderCSV(w io.Writer, events []json.RawMessage, fields []string) error {
	if len(fields) == 0 {
		fields = []string{"metadata.event_timestamp", "metadata.event_type"}
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(fields); err != nil {
		return err
	}
	for _, ev := range events {
		row := make([]string, len(fields))
		for i, f := range fields {
			row[i] = extractUDMField(ev, f)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// renderTable prints an aligned table. With no --fields it falls back to the
// timestamp/event-type summary; with --fields it projects those columns.
func renderTable(w io.Writer, events []json.RawMessage, fields []string) error {
	if len(fields) == 0 {
		fmt.Fprintf(w, "UDM search returned %d event(s).\n", len(events))
		for i, ev := range events {
			when, etype := udmSummary(ev)
			fmt.Fprintf(w, "  %4d  %s  %s\n", i+1, when, etype)
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(fields, "\t"))
	for _, ev := range events {
		cols := make([]string, len(fields))
		for i, f := range fields {
			cols[i] = extractUDMField(ev, f)
		}
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
	}
	return tw.Flush()
}

// extractUDMField navigates a dotted UDM path (e.g. "principal.hostname") into a
// raw event and returns a flat string. It tolerates the event shapes the API
// emits — {"udm":{…}} (:udmSearch), {"event":{…}} (UdmEventInfo), or a bare UDM
// object — and matches each segment case-insensitively across camelCase /
// snake_case. Missing fields render as "".
func extractUDMField(ev json.RawMessage, path string) string {
	var top map[string]any
	if err := json.Unmarshal(ev, &top); err != nil {
		return ""
	}
	root := top
	if u, ok := top["udm"].(map[string]any); ok {
		root = u
	} else if e, ok := top["event"].(map[string]any); ok {
		root = e
	}
	var cur any = root
	for seg := range strings.SplitSeq(path, ".") {
		// Auto-enter a singleton array so principal.ipGeoArtifact.network.asn
		// resolves without an explicit [0] segment.
		if arr, ok := cur.([]any); ok && len(arr) == 1 {
			cur = arr[0]
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = lookupKey(m, seg)
		if !ok {
			return ""
		}
	}
	return flattenValue(cur)
}

// extractJSONPath navigates a dotted path into decoded JSON and returns a flat
// string. Unlike extractUDMField it walks ARRAYS too — a numeric segment indexes
// into one (protoPayload.metadata.event.0.parameter) — so it suits raw-log JSON,
// whose nesting is arbitrary. Map segments match camelCase/snake_case like
// --fields. Missing paths render as "".
func extractJSONPath(doc any, path string) string {
	cur := doc
	for seg := range strings.SplitSeq(path, ".") {
		switch t := cur.(type) {
		case map[string]any:
			v, ok := lookupKey(t, seg)
			if !ok {
				return ""
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(t) {
				return ""
			}
			cur = t[i]
		default:
			return ""
		}
	}
	return flattenValue(cur)
}

// lookupKey finds seg in m, trying the literal key then its camelCase/snake_case
// variants (UDM JSON arrives in either serialization).
func lookupKey(m map[string]any, seg string) (any, bool) {
	if v, ok := m[seg]; ok {
		return v, true
	}
	if v, ok := m[toCamel(seg)]; ok {
		return v, true
	}
	if v, ok := m[toSnake(seg)]; ok {
		return v, true
	}
	return nil, false
}

// flattenValue renders a leaf (or small collection) as a flat string: scalars as
// themselves, a scalar array as comma-joined, anything else as compact JSON.
func flattenValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// JSON numbers decode to float64; render integers without a decimal point.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		scalar := true
		for _, e := range t {
			switch e.(type) {
			case string, bool, float64, nil:
				parts = append(parts, flattenValue(e))
			default:
				scalar = false
			}
		}
		if scalar {
			return strings.Join(parts, ",")
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stdoutIsTerminal reports whether stdout is an interactive character device
// (mirrors stdinIsTerminal) — used to pick a human table vs agent-friendly JSONL.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// splitFields parses a comma-separated --fields value into trimmed, non-empty
// dotted UDM paths.
func splitFields(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for f := range strings.SplitSeq(s, ",") {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}
