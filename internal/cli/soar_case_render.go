package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"danny.vn/secops/soar/legacy"
)

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

type legacyCardsPage struct {
	Typed      []soarCaseCard
	Raw        []json.RawMessage
	TotalCount int
}

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
		if a.HasWorkflows {
			fmt.Fprintf(w, "       ▸ playbook(s) attached — timeline: cases wall --case-id %d ; faults: playbooks summary --case-id %d --alert %s\n",
				cs.ID, cs.ID, a.Identifier)
		}
	}
	return nil
}

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

func msToUTC(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}

// validateCaseFilter checks for common mistakes in --filter syntax. The modern
// cases API uses SQL-style operators (=, !=, AND, OR); OData-style tokens (eq,
// ne, gt, lt) are a frequent mistake from other platforms.
func validateCaseFilter(f string) error {
	f = strings.TrimSpace(f)
	if f == "" {
		return nil
	}
	for w := range strings.FieldsSeq(f) {
		switch strings.ToLower(w) {
		case "eq", "ne", "gt", "lt", "ge", "le":
			return fmt.Errorf("--filter uses SQL-style syntax (= != > < >= <=), not OData — "+
				"got %q; example: --filter \"status = 'OPENED'\"", w)
		}
	}
	return nil
}
