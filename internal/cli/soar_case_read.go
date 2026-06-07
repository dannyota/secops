package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// Read layer for `soar case` — the query half of the SOAR operational loop
// (query -> review -> act). These verbs hit the reliable AppKey external API and
// only read, so they are unguarded; the mutating verbs live in soar_case.go.
// caseId is the SOAR INTEGER id (not the SIEM UUID); see the dual case-id gotcha
// in soar/legacy/cases.go.

// soarCaseCard is the subset of a case-queue card we render in `list`. The live
// payload is larger and schema-unstable, so only the table columns are decoded
// (--json emits the full body).
type soarCaseCard struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	Priority         int    `json:"priority"`
	Status           int    `json:"status"`
	Stage            string `json:"stage"`
	AssignedUserName string `json:"assignedUserName"`
	AlertsCount      int    `json:"alertsCount"`
	CreationTimeMs   int64  `json:"creationTimeUnixTimeInMs"`
}

// soarCaseQueueResponse is the GetCaseCardsByRequest page envelope.
type soarCaseQueueResponse struct {
	CaseCards  []soarCaseCard `json:"caseCards"`
	TotalCount int            `json:"totalCount"`
}

// soarAlertCard is the subset of a case alert we render under `get`. The legacy
// payload leaves the integer `id` unset; `identifier` is the canonical key and
// the value the mutating verbs' --alert flag consumes, so that is what we show.
type soarAlertCard struct {
	Identifier  string `json:"identifier"`
	Name        string `json:"name"`
	Product     string `json:"product"`
	Priority    int    `json:"priority"`
	StartTimeMs int64  `json:"startTimeUnixTimeInMs"`
}

// soarCaseFull is the subset of GetCaseFullDetails we render: the case header
// plus its alerts.
type soarCaseFull struct {
	ID               int             `json:"id"`
	Title            string          `json:"title"`
	Priority         int             `json:"priority"`
	Status           int             `json:"status"`
	Stage            string          `json:"stage"`
	AssignedUserName string          `json:"assignedUserName"`
	Environment      string          `json:"environment"`
	Description      string          `json:"description"`
	IsImportant      bool            `json:"isImportant"`
	IsIncident       bool            `json:"isIncident"`
	CreationTimeMs   int64           `json:"creationTimeUnixTimeInMs"`
	Alerts           []soarAlertCard `json:"alerts"`
}

func newCaseListCmd() *cobra.Command {
	var (
		status string
		limit  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list SOAR cases (default: open)",
		Long: "List SOAR cases (first page) as a compact table, or raw JSON. Reads only —\n" +
			"no LIVE banner. Use the case id shown here with `soar case get` and the\n" +
			"mutating verbs.\n\n" +
			"Uses the modern v1alpha cases API by default, falling back to the legacy\n" +
			"AppKey queue on error. --legacy forces the legacy queue only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses, err := parseSOARCaseStatuses(status)
			if err != nil {
				return err
			}
			pageSize := limit
			if pageSize <= 0 {
				pageSize = 100
			}
			return preferModern("soar case list",
				func() error { return runModernCaseList(pageSize, status, asJSON) },
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.ListCaseCards(baseContext(), legacy.CaseQueueRequest{
						RequestedPage: 0, PageSize: pageSize, Statuses: statuses,
					})
					if err != nil {
						return err
					}
					if asJSON {
						return writeRawJSON(os.Stdout, raw)
					}
					return emitSOARCaseCards(os.Stdout, raw, limit)
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "open", "status filter: open|closed|all")
	f.IntVar(&limit, "limit", 100, "max cases to fetch/show (first page; 0 = up to 100)")
	f.BoolVar(&asJSON, "json", false, "emit the raw queue JSON")
	return cmd
}

// runModernCaseList lists cases via the modern v1alpha API (SOAR host), fetching
// before printing so a fetch error falls back cleanly. --status maps to the modern
// Status (OPENED/CLOSED) and is pushed **server-side** via filter (open→OPENED,
// closed→CLOSED, all→no filter); the same status is re-checked client-side as a
// safety net in case the server ignores the filter. orderBy + expand mirror the
// web UI's request for stable ordering and richer `--json` output.
func runModernCaseList(pageSize int, status string, asJSON bool) error {
	c, err := newSOARClient()
	if err != nil {
		return err
	}
	want := ""
	switch status {
	case "open":
		want = "OPENED"
	case "closed":
		want = "CLOSED"
	}
	opt := soar.CaseListOptions{
		PageSize: pageSize,
		OrderBy:  "updateTime desc",
		Expand:   "products,tasks,tags,closureDetails,sla,alertsSla",
	}
	if want != "" {
		opt.Filter = "status = '" + want + "'"
	}
	raws, err := c.ListCasesOpts(baseContext(), opt)
	if err != nil {
		return err // preferModern falls back to legacy
	}
	type row struct {
		id, priority, stat, stage string
	}
	var rows []row
	kept := make([]json.RawMessage, 0, len(raws))
	for _, r := range raws {
		var cs soar.Case
		if err := json.Unmarshal(r, &cs); err != nil {
			continue
		}
		if want != "" && !strings.EqualFold(cs.Status, want) {
			continue
		}
		id := cs.DisplayID
		if id == "" { // modern payload keys the id under the resource name
			id = cs.Name[strings.LastIndex(cs.Name, "/")+1:]
		}
		rows = append(rows, row{id, cs.Priority, cs.Status, cs.Stage})
		kept = append(kept, r)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(kept)
	}
	for _, rw := range rows {
		fmt.Fprintf(os.Stdout, "%-14s %-16s %-8s %s\n", rw.id, rw.priority, rw.stat, rw.stage)
	}
	fmt.Fprintf(os.Stdout, "\n%d case(s) (modern v1alpha)\n", len(rows))
	return nil
}

func newCaseGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <case-id>",
		Short: "Read-only: get one SOAR case and its alerts",
		Long: "Fetch a single case by its SOAR integer id (GetCaseFullDetails) and show\n" +
			"the case header plus its alerts, or the raw JSON with --json.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("case id must be an integer, got %q", args[0])
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.GetCaseFullDetails(baseContext(), id)
			if err != nil {
				return err
			}
			if asJSON {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitSOARCaseFull(os.Stdout, raw)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the raw full-details JSON")
	return cmd
}

// parseSOARCaseStatuses maps the --status flag to the legacy numeric status codes.
func parseSOARCaseStatuses(s string) ([]int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "open":
		return []int{legacy.CaseStatusOpen}, nil
	case "closed":
		return []int{legacy.CaseStatusClosed}, nil
	case "all":
		return []int{legacy.CaseStatusOpen, legacy.CaseStatusClosed}, nil
	default:
		return nil, fmt.Errorf("invalid --status %q (want open|closed|all)", s)
	}
}

// emitSOARCaseCards renders the decoded queue page as a compact table.
func emitSOARCaseCards(w io.Writer, raw json.RawMessage, limit int) error {
	var resp soarCaseQueueResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode case cards: %w", err)
	}
	cards := resp.CaseCards
	if limit > 0 && len(cards) > limit {
		cards = cards[:limit]
	}
	if len(cards) == 0 {
		fmt.Fprintln(w, "no cases.")
		return nil
	}
	fmt.Fprintf(w, "%-10s %-12s %-7s %-18s %-7s %-18s %s\n",
		"ID", "PRIORITY", "STATUS", "STAGE", "ALERTS", "ASSIGNEE", "TITLE")
	for i := range cards {
		c := &cards[i]
		fmt.Fprintf(w, "%-10d %-12s %-7s %-18s %-7d %-18s %s\n",
			c.ID, soarPriorityName(c.Priority), soarCaseStatusName(c.Status),
			truncate(orDash(c.Stage), 17), c.AlertsCount,
			truncate(orDash(c.AssignedUserName), 17), truncate(c.Title, 50))
	}
	fmt.Fprintf(w, "\n%d case(s)", len(cards))
	if resp.TotalCount > len(cards) {
		fmt.Fprintf(w, " (of %d total)", resp.TotalCount)
	}
	fmt.Fprintln(w, ".")
	return nil
}

// emitSOARCaseFull renders the case header plus its alerts.
func emitSOARCaseFull(w io.Writer, raw json.RawMessage) error {
	var cs soarCaseFull
	if err := json.Unmarshal(raw, &cs); err != nil {
		return fmt.Errorf("decode case details: %w", err)
	}
	fmt.Fprintf(w, "Case %d  —  %s\n", cs.ID, orDash(cs.Title))
	fmt.Fprintf(w, "  Priority:    %s\n", soarPriorityName(cs.Priority))
	fmt.Fprintf(w, "  Status:      %s\n", soarCaseStatusName(cs.Status))
	fmt.Fprintf(w, "  Stage:       %s\n", orDash(cs.Stage))
	fmt.Fprintf(w, "  Assignee:    %s\n", orDash(cs.AssignedUserName))
	fmt.Fprintf(w, "  Environment: %s\n", orDash(cs.Environment))
	fmt.Fprintf(w, "  Created:     %s\n", msToUTC(cs.CreationTimeMs))
	fmt.Fprintf(w, "  Important:   %v    Incident: %v\n", cs.IsImportant, cs.IsIncident)
	if d := strings.TrimSpace(cs.Description); d != "" {
		fmt.Fprintf(w, "  Description: %s\n", truncate(d, 120))
	}

	fmt.Fprintf(w, "\n  Alerts (%d):\n", len(cs.Alerts))
	if len(cs.Alerts) == 0 {
		fmt.Fprintln(w, "    none.")
		return nil
	}
	// One block per alert: a summary line plus the verbatim --alert identifier
	// (too long for a table column, and the value the mutate verbs need).
	for i := range cs.Alerts {
		a := &cs.Alerts[i]
		fmt.Fprintf(w, "    %d. %s  —  %s · %s · %s\n",
			i+1, orDash(a.Name), soarPriorityName(a.Priority), orDash(a.Product), msToUTC(a.StartTimeMs))
		if a.Identifier != "" {
			fmt.Fprintf(w, "       --alert %s\n", a.Identifier)
		}
	}
	return nil
}

// soarPriorityName maps the SOAR CasePriority code to its label, falling back to
// the raw number for any unmapped value.
func soarPriorityName(p int) string {
	switch p {
	case -1:
		return "Informative"
	case 40:
		return "Low"
	case 60:
		return "Medium"
	case 80:
		return "High"
	case 100:
		return "Critical"
	default:
		return strconv.Itoa(p)
	}
}

// soarCaseStatusName maps the legacy CaseDataStatus code to OPEN/CLOSED, falling
// back to the raw number for any other value.
func soarCaseStatusName(s int) string {
	switch s {
	case legacy.CaseStatusOpen:
		return "OPEN"
	case legacy.CaseStatusClosed:
		return "CLOSED"
	default:
		return strconv.Itoa(s)
	}
}

// msToUTC formats a Unix-millis timestamp as a compact UTC time, or "-" if unset.
func msToUTC(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}
