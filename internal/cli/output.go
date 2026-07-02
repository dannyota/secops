package cli

// output.go — the shared format layer for commands that render tabular data.
// The global --output flag (root.go) picks table | json | csv; the format-aware
// commands with a richer local --format flag (query results, mitre, rules
// health) let the local flag win.

import (
	"encoding/csv"
	"io"
)

// effectiveFormat resolves a format-aware command's output format: the local
// --format wins, then the global --output, then --json means json. An empty
// result means "no preference" — the caller applies its own default (table, or
// terminal-dependent for query results).
func effectiveFormat(local string) string {
	if local != "" {
		return local
	}
	if outputFormat != "" {
		return outputFormat
	}
	if jsonOut {
		return "json"
	}
	return ""
}

// printCSVTo writes one header row plus data rows as RFC-4180 CSV.
func printCSVTo(w io.Writer, header []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write(r); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
