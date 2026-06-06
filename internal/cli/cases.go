package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// The `cases` command is the SIEM operational plane: query and triage live cases
// (SIEM cases are UUIDs — distinct from SOAR integer-id cases under `soar case`).
// Reads are free; every mutation is a guarded production action (dry-run default,
// --yes to apply), with bulk acting on a reviewed --ids set or a --filter that is
// dry-run-first and --limit-capped. See docs/SIEM-DESIGN.md.

func init() {
	rootCmd.AddCommand(newCasesCmd())
}

func newCasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cases <verb>",
		Short: "Query and triage live SIEM cases (UUID-keyed; distinct from `soar case`)",
		Long: "Operational SIEM case workflow: list/search/get to query, then act per-item\n" +
			"or on a subset. Mutations are guarded (dry-run by default, --yes to apply).",
	}
	cmd.AddCommand(
		newCasesListCmd(),
		newCasesGetCmd(),
		newCasesSearchCmd(),
	)
	return cmd
}

// --- read layer -------------------------------------------------------------

func newCasesListCmd() *cobra.Command {
	var (
		filter   string
		orderBy  string
		expand   string
		pageSize int
		limit    int
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cases (v1beta collection), newest-first by default",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if orderBy == "" {
				orderBy = "createTime desc"
			}
			cases, err := c.ListCasesOpts(baseContext(), chronicle.CaseListOptions{
				Filter: filter, OrderBy: orderBy, Expand: expand, PageSize: pageSize,
			})
			if err != nil {
				return err
			}
			cases = capCases(cases, limit)
			return emitCases(os.Stdout, cases, asJSON)
		},
	}
	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "case filter expression")
	f.StringVar(&orderBy, "order-by", "", "comma-separated sort (default: createTime desc)")
	f.StringVar(&expand, "expand", "", "expand fields, e.g. tags,products")
	f.IntVar(&pageSize, "page-size", 0, "per-page cap (server default if 0)")
	f.IntVar(&limit, "limit", 100, "max cases to return (0 = no cap)")
	f.BoolVar(&asJSON, "json", false, "emit raw JSON (one object per case, for scripting)")
	return cmd
}

func newCasesGetCmd() *cobra.Command {
	var (
		expand string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "get <case-id>",
		Short: "Get a single case by id (UUID) or resource name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			cs, err := c.GetCase(baseContext(), args[0], expand)
			if err != nil {
				return err
			}
			if asJSON {
				return writeRawJSON(os.Stdout, cs.Raw)
			}
			return emitCases(os.Stdout, []chronicle.Case{*cs}, false)
		},
	}
	f := cmd.Flags()
	f.StringVar(&expand, "expand", "", "expand fields, e.g. tags,products,events")
	f.BoolVar(&asJSON, "json", false, "emit the raw case JSON")
	return cmd
}

func newCasesSearchCmd() *cobra.Command {
	var (
		hours    int
		ids      string
		pageSize int
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Legacy time-windowed case search (v1alpha); returns the raw page",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			s := chronicle.CaseSearch{PageSize: pageSize}
			if hours > 0 {
				s.EndTime = time.Now().UTC()
				s.StartTime = s.EndTime.Add(-time.Duration(hours) * time.Hour)
			}
			if ids != "" {
				s.CaseIDs = splitCSV(ids)
			}
			raw, err := c.SearchCases(baseContext(), s)
			if err != nil {
				return err
			}
			// The legacy page is instance-shaped; emit it raw either way.
			_ = asJSON
			return writeRawJSON(os.Stdout, raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 0, "look back this many hours (createTime window)")
	f.StringVar(&ids, "ids", "", "comma-separated case ids to fetch")
	f.IntVar(&pageSize, "page-size", 100, "page size")
	f.BoolVar(&asJSON, "json", true, "emit raw JSON (always, for the legacy page)")
	return cmd
}

// --- helpers ----------------------------------------------------------------

// capCases trims the slice to limit (0 = no cap).
func capCases(cases []chronicle.Case, limit int) []chronicle.Case {
	if limit > 0 && len(cases) > limit {
		return cases[:limit]
	}
	return cases
}

// emitCases prints cases as a compact table, or as a raw JSON array under --json.
func emitCases(w io.Writer, cases []chronicle.Case, asJSON bool) error {
	if asJSON {
		return writeCasesJSON(w, cases)
	}
	if len(cases) == 0 {
		fmt.Fprintln(w, "no cases.")
		return nil
	}
	fmt.Fprintf(w, "%-38s %-16s %-9s %-22s %s\n", "ID", "PRIORITY", "STATUS", "ASSIGNEE", "TITLE")
	for i := range cases {
		c := &cases[i]
		fmt.Fprintf(w, "%-38s %-16s %-9s %-22s %s\n",
			c.CaseID(), trimPriority(c.Priority), orDash(c.Status), orDash(c.Assignee), truncate(c.DisplayName, 60))
	}
	fmt.Fprintf(w, "\n%d case(s).\n", len(cases))
	return nil
}

// writeCasesJSON emits the cases' raw server objects as a JSON array.
func writeCasesJSON(w io.Writer, cases []chronicle.Case) error {
	parts := make([]json.RawMessage, 0, len(cases))
	for i := range cases {
		if len(cases[i].Raw) > 0 {
			parts = append(parts, cases[i].Raw)
		}
	}
	b, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	return writeRawJSON(w, b)
}

// writeRawJSON pretty-prints raw JSON to w with a trailing newline.
func writeRawJSON(w io.Writer, raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		_, err = w.Write(raw)
		return err
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

func trimPriority(p chronicle.CasePriority) string {
	return strings.TrimPrefix(string(p), "PRIORITY_")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// splitCSV splits "a,b,c" into a trimmed, non-empty slice.
func splitCSV(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// truncate shortens s to at most n runes, with an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
