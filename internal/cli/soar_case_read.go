package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
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

// soarAlertCard is the subset of a case alert we render under `get`. The legacy
// payload leaves the integer `id` unset; `identifier` is the canonical key and
// the value the mutating verbs' --alert flag consumes, so that is what we show.
// additionalProperties carries the detection-rule linkage: ruleGenerator (the
// rule display name) and rule_id (the `ru_` id) — the pivot into the SIEM
// rule-tuning verbs, which accept either form.
type soarAlertCard struct {
	Identifier           string `json:"identifier"`
	Name                 string `json:"name"`
	Product              string `json:"product"`
	Priority             int    `json:"priority"`
	StartTimeMs          int64  `json:"startTimeUnixTimeInMs"`
	AdditionalProperties struct {
		RuleGenerator string `json:"ruleGenerator"`
		RuleID        string `json:"rule_id"`
	} `json:"additionalProperties"`
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

// caseListFilters are the triage filters of `soar case list` beyond --status.
// Passed by value (the zero value means "no filters"). assignee/since apply
// client-side over the fetched page on BOTH lanes; priority is pushed
// server-side on the modern lane (and re-checked client-side, like --status);
// tag/rawFilter are modern-lane only and fail loud on the legacy queue.
type caseListFilters struct {
	assignee  string              // case-insensitive substring of the assignee
	priority  legacy.CasePriority // 0 = unset (the typed server coding, both lanes)
	tag       string              // exact tag (case-insensitive); modern lane only
	since     time.Time           // keep cases updated/created at-or-after this instant
	rawFilter string              // verbatim modern server-side filter expression
}

func (f caseListFilters) active() bool {
	return f.assignee != "" || f.priority != 0 || f.tag != "" || !f.since.IsZero()
}

// modernOnly reports whether a filter that exists only on the modern cases API
// is set (the legacy queue can neither serve nor approximate it).
func (f caseListFilters) modernOnly() bool { return f.tag != "" || f.rawFilter != "" }

// modernPriorityToken maps the typed CasePriority to the modern v1alpha wire
// token (the PRIORITY_* vocabulary the cases API filters and returns).
func modernPriorityToken(p legacy.CasePriority) string {
	switch p {
	case legacy.PriorityInformative:
		return "PRIORITY_INFO"
	case legacy.PriorityLow:
		return "PRIORITY_LOW"
	case legacy.PriorityMedium:
		return "PRIORITY_MEDIUM"
	case legacy.PriorityHigh:
		return "PRIORITY_HIGH"
	case legacy.PriorityCritical:
		return "PRIORITY_CRITICAL"
	}
	return ""
}

// parseSince accepts a look-back duration ("30m", "24h") or an absolute
// timestamp (RFC3339 / ISO-8601 / YYYY-MM-DD, per parseQueryTS) and returns the
// cut-off instant.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().UTC().Add(-d), nil
	}
	if t, err := parseQueryTS(s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (use a duration like 24h, or RFC3339 / YYYY-MM-DD)", s)
}

func newCaseListCmd() *cobra.Command {
	var (
		status   string
		limit    int
		assignee string
		priority string
		tag      string
		since    string
		filter   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list SOAR cases (default: open) with triage filters",
		Long: "List SOAR cases (first page) as a compact table, or raw JSON. Reads only —\n" +
			"no LIVE banner. Use the case id shown here with `soar case get` and the\n" +
			"mutating verbs.\n\n" +
			"Triage filters: --assignee (substring), --priority, --tag, --since narrow the\n" +
			"fetched page client-side on both lanes (--tag needs the modern lane; --since\n" +
			"matches the case's update time, or creation time on the legacy queue).\n" +
			"--filter passes a verbatim server-side filter expression to the modern\n" +
			"v1alpha cases API (e.g. \"status = 'OPENED'\").\n\n" +
			"Uses the modern v1alpha cases API by default, falling back to the legacy\n" +
			"AppKey queue on error. --legacy forces the legacy queue only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses, err := parseSOARCaseStatuses(status)
			if err != nil {
				return err
			}
			var prio legacy.CasePriority
			if strings.TrimSpace(priority) != "" {
				if prio, err = legacy.ParseCasePriority(priority); err != nil {
					return err
				}
			}
			cutoff, err := parseSince(since)
			if err != nil {
				return err
			}
			filters := caseListFilters{
				assignee: strings.TrimSpace(assignee), priority: prio,
				tag: strings.TrimSpace(tag), since: cutoff, rawFilter: strings.TrimSpace(filter),
			}
			pageSize := limit
			if pageSize <= 0 {
				pageSize = 100
			}
			// --tag/--filter exist only on the modern cases API: run it directly so
			// a modern-lane error surfaces as-is (a fallback could only fail with a
			// misleading "requires the modern lane" message), and refuse --legacy.
			if filters.modernOnly() {
				if forceLegacy {
					return fmt.Errorf("--tag/--filter require the modern cases lane (remove --legacy)")
				}
				return runModernCaseList(pageSize, status, jsonOut, filters)
			}
			return preferModern("soar case list",
				func() error { return runModernCaseList(pageSize, status, jsonOut, filters) },
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
					if jsonOut {
						return emitSOARCaseCardsJSON(os.Stdout, raw, filters)
					}
					return emitSOARCaseCards(os.Stdout, raw, limit, filters)
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "open", "status filter: open|closed|all")
	f.IntVar(&limit, "limit", 100, "max cases to fetch/show (first page; 0 = up to 100)")
	f.StringVar(&assignee, "assignee", "", "keep cases whose assignee contains this (case-insensitive)")
	f.StringVar(&priority, "priority", "", "keep cases at this priority: informative|low|medium|high|critical")
	f.StringVar(&tag, "tag", "", "keep cases carrying this tag (modern lane only)")
	f.StringVar(&since, "since", "", "keep cases updated since (duration like 24h, RFC3339, or YYYY-MM-DD)")
	f.StringVar(&filter, "filter", "", "verbatim server-side filter for the modern cases API")
	return markJSON(cmd)
}

// runModernCaseList lists cases via the modern v1alpha API (SOAR host), fetching
// before printing so a fetch error falls back cleanly. --status and --priority
// map to modern wire tokens and are pushed **server-side** via filter (both are
// confirmed honored; each is re-checked client-side as a safety net); --filter
// is ANDed in verbatim. The remaining triage filters narrow the fetched page
// client-side. orderBy + expand mirror the web UI's request for stable ordering
// and richer `--json` output.
func runModernCaseList(pageSize int, status string, asJSON bool, filters caseListFilters) error {
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
		MaxItems: pageSize, // "first page" semantics — bound the fetch, don't page the whole tenant
		OrderBy:  "updateTime desc",
		Expand:   "products,tasks,tags,closureDetails,sla,alertsSla",
	}
	var parts []string
	if want != "" {
		parts = append(parts, "status = '"+want+"'")
	}
	if tok := modernPriorityToken(filters.priority); tok != "" {
		parts = append(parts, "priority = '"+tok+"'")
	}
	if filters.rawFilter != "" {
		parts = append(parts, filters.rawFilter)
	}
	opt.Filter = strings.Join(parts, " AND ")
	cases, err := c.ListCasesTyped(baseContext(), opt)
	if err != nil {
		return err // preferModern falls back to legacy
	}
	fetched := len(cases)
	kept := make([]soar.Case, 0, len(cases))
	for _, cs := range cases {
		if want != "" && !strings.EqualFold(cs.Status, want) {
			continue
		}
		if !matchModernCase(&cs, filters) {
			continue
		}
		kept = append(kept, cs)
	}
	if asJSON {
		raws := make([]json.RawMessage, 0, len(kept))
		for _, cs := range kept {
			raws = append(raws, cs.Raw)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raws)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tPRIORITY\tSTATUS\tSTAGE\tASSIGNEE")
	for _, cs := range kept {
		id := cs.DisplayID
		if id == "" { // modern payload keys the id under the resource name
			id = cs.Name[strings.LastIndex(cs.Name, "/")+1:]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			id, truncate(cs.Title, 40), prettyPriority(cs.Priority), cs.Status, cs.Stage, dashIfEmpty(cs.Assignee))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\n%d case(s) (modern v1alpha)", len(kept))
	if filters.active() && len(kept) < fetched {
		// The filters narrow ONE fetched page — older matches may exist beyond it.
		fmt.Fprintf(os.Stdout, " — filtered from the first %d by update time; raise --limit to widen", fetched)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// matchModernCase applies the client-side triage filters to a modern typed case.
// Tags and the update time are not in the typed view, so one probe decode of the
// raw body serves both checks (the tags array has carried both plain strings and
// objects across schema revisions).
func matchModernCase(cs *soar.Case, f caseListFilters) bool {
	if !f.active() {
		return true
	}
	if f.assignee != "" && !strings.Contains(strings.ToLower(cs.Assignee), strings.ToLower(f.assignee)) {
		return false
	}
	if f.priority != 0 && modernPriorityToken(f.priority) != strings.TrimSpace(cs.Priority) {
		return false
	}
	if f.tag == "" && f.since.IsZero() {
		return true
	}
	var probe struct {
		Tags       []json.RawMessage `json:"tags"`
		UpdateTime string            `json:"updateTime"`
		CreateTime string            `json:"createTime"`
	}
	if err := json.Unmarshal(cs.Raw, &probe); err != nil {
		return false
	}
	if f.tag != "" && !slices.ContainsFunc(probe.Tags, func(t json.RawMessage) bool {
		return strings.EqualFold(tagValue(t), f.tag)
	}) {
		return false
	}
	if !f.since.IsZero() {
		ts := time.Time{}
		for _, v := range []string{probe.UpdateTime, probe.CreateTime} {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				ts = t.UTC()
				break
			}
		}
		if ts.IsZero() || ts.Before(f.since) {
			return false
		}
	}
	return true
}

// tagValue extracts one tag element's value — a bare string, or the first of
// displayName/name/tag on an object element.
func tagValue(t json.RawMessage) string {
	var s string
	if json.Unmarshal(t, &s) == nil {
		return s
	}
	var obj struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
		Tag         string `json:"tag"`
	}
	if json.Unmarshal(t, &obj) != nil {
		return ""
	}
	for _, v := range []string{obj.DisplayName, obj.Name, obj.Tag} {
		if v != "" {
			return v
		}
	}
	return ""
}

// prettyPriority renders a modern priority token (e.g. "PRIORITY_HIGH") as a short
// label ("High"); unknown/empty values pass through (a dash for empty).
func prettyPriority(p string) string {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(p, "PRIORITY_")), "_", " ")
	if s == "" {
		return "-"
	}
	return strings.ToUpper(s[:1]) + s[1:] // ASCII enum token; cap the first letter
}

// dashIfEmpty renders "-" for an empty cell so columns read cleanly.
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newCaseGetCmd() *cobra.Command {
	var idFlag int
	cmd := &cobra.Command{
		Use:   "get <case-id>",
		Short: "Read-only: get one SOAR case and its alerts",
		Long: "Fetch a single case by its SOAR integer id (GetCaseFullDetails) and show\n" +
			"the case header plus its alerts, or the raw JSON with --json. The id can be\n" +
			"given positionally or with --id (symmetry with the mutating verbs).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := idFlag
			if len(args) == 1 {
				n, perr := strconv.Atoi(strings.TrimSpace(args[0]))
				if perr != nil {
					return fmt.Errorf("case id must be an integer, got %q", args[0])
				}
				id = n
			}
			if id == 0 {
				return fmt.Errorf("a case id is required (positional or --id)")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.GetCaseFullDetails(baseContext(), id)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitSOARCaseFull(os.Stdout, raw)
		},
	}
	cmd.Flags().IntVar(&idFlag, "id", 0, "SOAR case id (alternative to the positional arg)")
	return markJSON(cmd)
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

// matchLegacyCard applies the client-side triage filters to a legacy queue card
// (tags/rawFilter are rejected before this path; --since matches the card's
// creation time, the only timestamp the card carries).
func matchLegacyCard(c *soarCaseCard, f caseListFilters) bool {
	if !f.active() {
		return true
	}
	if f.assignee != "" && !strings.Contains(strings.ToLower(c.AssignedUserName), strings.ToLower(f.assignee)) {
		return false
	}
	if f.priority != 0 && c.Priority != int(f.priority) {
		return false
	}
	if !f.since.IsZero() {
		if c.CreationTimeMs <= 0 || time.UnixMilli(c.CreationTimeMs).UTC().Before(f.since) {
			return false
		}
	}
	return true
}

// legacyCardsPage is a decoded queue page holding each card both typed (for the
// table and the filter match) and raw (so --json stays lossless under filters).
type legacyCardsPage struct {
	Typed      []soarCaseCard
	Raw        []json.RawMessage
	TotalCount int
}

// filterLegacyCards decodes the queue page and drops cards the filters exclude,
// keeping the typed and raw views in step.
func filterLegacyCards(raw json.RawMessage, f caseListFilters) (*legacyCardsPage, error) {
	var resp struct {
		CaseCards  []json.RawMessage `json:"caseCards"`
		TotalCount int               `json:"totalCount"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode case cards: %w", err)
	}
	page := &legacyCardsPage{TotalCount: resp.TotalCount}
	for _, rec := range resp.CaseCards {
		var card soarCaseCard
		if err := json.Unmarshal(rec, &card); err != nil {
			return nil, fmt.Errorf("decode case card: %w", err)
		}
		if matchLegacyCard(&card, f) {
			page.Typed = append(page.Typed, card)
			page.Raw = append(page.Raw, rec)
		}
	}
	return page, nil
}

// emitSOARCaseCardsJSON emits the queue page under --json: the raw page when no
// filter is active (existing behavior), or the same envelope shape with only the
// matching cards — each card stays the full raw server record either way.
func emitSOARCaseCardsJSON(w io.Writer, raw json.RawMessage, f caseListFilters) error {
	if !f.active() {
		return writeRawJSON(w, raw)
	}
	page, err := filterLegacyCards(raw, f)
	if err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		CaseCards  []json.RawMessage `json:"caseCards"`
		TotalCount int               `json:"totalCount"`
	}{CaseCards: page.Raw, TotalCount: page.TotalCount})
	if err != nil {
		return err
	}
	return writeRawJSON(w, b)
}

// emitSOARCaseCards renders the decoded queue page as a compact table.
func emitSOARCaseCards(w io.Writer, raw json.RawMessage, limit int, f caseListFilters) error {
	page, err := filterLegacyCards(raw, f)
	if err != nil {
		return err
	}
	cards := page.Typed
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
	if page.TotalCount > len(cards) {
		fmt.Fprintf(w, " (of %d total)", page.TotalCount)
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
	// (too long for a table column, and the value the mutate verbs need) and the
	// firing rule — the pivot into rule tuning (`rules detections <name>` takes
	// the display name as-is).
	for i := range cs.Alerts {
		a := &cs.Alerts[i]
		fmt.Fprintf(w, "    %d. %s  —  %s · %s · %s\n",
			i+1, orDash(a.Name), soarPriorityName(a.Priority), orDash(a.Product), msToUTC(a.StartTimeMs))
		if a.Identifier != "" {
			fmt.Fprintf(w, "       --alert %s\n", a.Identifier)
		}
		if rule := a.AdditionalProperties.RuleGenerator; rule != "" {
			line := "       rule: " + rule
			if id := a.AdditionalProperties.RuleID; id != "" {
				line += "  (" + id + ")"
			}
			fmt.Fprintf(w, "%s   — tune: rules detections %q\n", line, rule)
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
