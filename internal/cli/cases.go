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

// The `cases` command reaches a case on the Chronicle host by UUID (ADC). This is
// the ALTERNATE path: the chronicle.googleapis.com cases collection currently
// HTTP-500s at every API version, so for case work prefer `soar case` — the same
// case on the SOAR host, where it works (modern v1alpha list + the reliable AppKey
// verbs). Kept here as the typed alternate-path reader; reads are free. There is
// one case reachable by several APIs, not two case systems. See docs/design/siem.md.

func init() {
	rootCmd.AddCommand(newCasesCmd())
}

func newCasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cases <verb>",
		Short: "Read a case on the Chronicle host by UUID — alternate path; prefer `soar case`",
		Long: "Reach a case on the Chronicle host (chronicle.googleapis.com, ADC) by UUID.\n" +
			"This collection currently 500s at every API version, so for case work use\n" +
			"`soar case` — the same case on the SOAR host, where it works. Reads only here.",
	}
	cmd.AddCommand(
		newCasesListCmd(),
		newCasesGetCmd(),
		newCasesSearchCmd(),
		newCasesSoarIDCmd(),
	)
	return cmd
}

// newCasesSoarIDCmd bridges SIEM case uuids to SOAR integer case ids — the id
// every `soar case` verb needs. This is the working read on the chronicle host
// (legacyBatchGetCases), unlike the 500ing cases collection above.
func newCasesSoarIDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soar-id <case-uuid> [<case-uuid>...]",
		Short: "Read-only: resolve SIEM case uuid(s) to SOAR integer case id(s)",
		Long: "Resolve one or more SIEM case uuids (e.g. an alert's caseName from\n" +
			"`alerts list --json`) to their SOAR integer case ids via legacyBatchGetCases —\n" +
			"the id `soar case get` and the mutating case verbs take. One case, two ids:\n" +
			"this is the bridge between them.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uuids := make([]string, 0, len(args))
			for _, a := range args {
				uuids = append(uuids, splitCSV(a)...)
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			resp, err := c.BatchGetCases(baseContext(), uuids)
			if err != nil {
				return err
			}
			rows := soarIDRows(uuids, resp.Cases)
			if jsonOut {
				return emitJSON(rows)
			}
			fmt.Fprintf(os.Stdout, "%-38s %s\n", "SIEM CASE UUID", "SOAR CASE ID")
			for _, r := range rows {
				fmt.Fprintf(os.Stdout, "%-38s %s\n", r.UUID, orDash(r.SOARCaseID))
			}
			return nil
		},
	}
	return markJSON(cmd)
}

// soarIDRow is one uuid -> SOAR-id mapping (the --json shape of `cases soar-id`).
type soarIDRow struct {
	UUID       string `json:"uuid"`
	SOARCaseID string `json:"soar_case_id"`
}

// soarIDRows pairs the requested uuids with the returned cases. A case that
// carries its own id (uuid or resource name) is paired by that key; cases whose
// bodies omit the id are paired positionally, but ONLY when the response is a
// clean 1:1 echo of the request (same length, nothing key-paired) — a partial or
// reordered response must not attribute one case's SOAR id to another uuid,
// since the resolved id feeds mutating `soar case` verbs.
func soarIDRows(uuids []string, cases []chronicle.LegacyCase) []soarIDRow {
	rows := make([]soarIDRow, len(uuids))
	for i, u := range uuids {
		rows[i] = soarIDRow{UUID: u}
	}
	soarID := func(cs *chronicle.LegacyCase) string {
		if cs.SoarPlatformInfo != nil {
			return cs.SoarPlatformInfo.CaseID
		}
		return ""
	}
	keyed := 0
	for i := range cases {
		cs := &cases[i]
		var probe struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(cs.Raw, &probe)
		key := probe.ID
		if key == "" {
			key = probe.Name[strings.LastIndex(probe.Name, "/")+1:]
		}
		if key == "" {
			continue
		}
		for j := range rows {
			if strings.EqualFold(rows[j].UUID, key) {
				rows[j].SOARCaseID = soarID(cs)
				keyed++
				break
			}
		}
	}
	if keyed == 0 && len(cases) == len(uuids) {
		for i := range cases {
			rows[i].SOARCaseID = soarID(&cases[i])
		}
	}
	return rows
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
		Short: "List cases on the Chronicle host (alternate path; 500s today — prefer `soar case list`)",
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
			total := len(cases)
			cases = capCases(cases, limit)
			// Don't let the cap masquerade as the full result set.
			if len(cases) < total {
				fmt.Fprintf(os.Stderr, "warning: showing %d of %d case(s) (--limit=%d); raise --limit for the rest.\n", len(cases), total, limit)
			}
			return emitCases(os.Stdout, cases, asJSON)
		},
	}
	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "case filter expression")
	f.StringVar(&orderBy, "order-by", "", "comma-separated sort (default: createTime desc)")
	f.StringVar(&expand, "expand", "", "expand fields, e.g. tags,products")
	f.IntVar(&pageSize, "page-size", 0, "per-page cap (server default if 0)")
	f.IntVar(&limit, "limit", 100, "max cases to return (0 = no cap)")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return markJSON(cmd)
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
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return markJSON(cmd)
}

func newCasesSearchCmd() *cobra.Command {
	var (
		hours    int
		ids      string
		pageSize int
	)
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Time-windowed case search via the chronicle legacy: RPC (legacyListCases 404s today)",
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
			// The legacy page is instance-shaped; it is always emitted raw.
			return writeRawJSON(os.Stdout, raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 0, "look back this many hours (createTime window)")
	f.StringVar(&ids, "ids", "", "comma-separated case ids to fetch")
	f.IntVar(&pageSize, "page-size", 100, "page size")
	// Output is always raw JSON (the instance-shaped legacy page).
	return markJSON(cmd)
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
