package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// `cases` is the single, canonical case command. A case is ONE record, not a SIEM
// case and a separate SOAR case; this command works it directly, auto-routed to the
// SOAR host (AppKey, the reliable lane) where every case verb answers. The verb
// tree is shared with the hidden back-compat `soar case` alias (see caseVerbs).
//
// `cases soar-id` is the one Chronicle-host (ADC) read kept here: it bridges a SIEM
// case UUID (an alert's caseName) to the SOAR integer id the other verbs take. The
// Chronicle-host cases collection itself errors at every API version, so it is not
// surfaced — there is one case reachable by several APIs, not two case systems.

func init() {
	rootCmd.AddCommand(newCasesCmd())
}

func newCasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cases <verb>",
		Short: "Work SOAR cases: read (list, get) + guarded triage (assign, tag, close, ...)",
		Long: "Operate cases against the live tenant. A case is one record; this command is\n" +
			"auto-routed to the SOAR host where every verb works (`soar case …` remains as\n" +
			"a hidden back-compat alias). `list` and `get` read only; every mutating verb\n" +
			"defaults to a dry run — pass --yes to apply.\n\n" +
			"caseId is the SOAR integer id, not the SIEM UUID. Bridge a UUID (e.g. an\n" +
			"alert's caseName) to its id with `cases soar-id`.",
	}
	cmd.AddCommand(caseVerbs()...)
	cmd.AddCommand(newCasesSoarIDCmd())
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

// --- helpers ----------------------------------------------------------------

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
