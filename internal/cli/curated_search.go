package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// curated search — unified search across rule sets AND individual rules.

type curatedSearchResult struct {
	RuleSets []enrichedRuleSet       `json:"ruleSets"`
	Rules    []chronicle.CuratedRule `json:"rules"`
}

func newCuratedSearchCmd() *cobra.Command {
	var (
		installed bool
		tactic    string
		severity  string
	)
	cmd := &cobra.Command{
		Use:   "search <query> [--installed] [--tactic T] [--severity S]",
		Short: "Read-only: search across curated rule sets AND individual rules",
		Long: "Unified search: matches <query> against rule-set names/descriptions AND\n" +
			"individual curated rule names/descriptions. Use --installed to restrict to\n" +
			"enabled (deployed) rule sets and their rules. Combine with --tactic (MITRE id\n" +
			"or name) and --severity to narrow further.\n\n" +
			"Structured filters work without a text query:\n" +
			"  curated search --tactic TA0005\n" +
			"  curated search --severity critical --installed",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = strings.TrimSpace(args[0])
			}
			if query == "" && tactic == "" && severity == "" {
				return fmt.Errorf("specify a search query, --tactic, or --severity")
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			sets, _, err := loadEnrichedRuleSets(ctx, c)
			if err != nil {
				return err
			}

			// Rule sets only match on text query — tactic/severity are
			// rule-level attributes. Skip set results when only structured
			// filters are used (no text query).
			var matchedSets []enrichedRuleSet
			if query != "" {
				matchedSets = searchRuleSets(sets, query, installed)
			}
			enabledSetIDs := enabledSetIDMap(sets)

			rules, err := c.ListCuratedRules(ctx)
			if err != nil {
				return err
			}
			matchedRules := searchRules(rules, query, tactic, severity, installed, enabledSetIDs)

			result := curatedSearchResult{RuleSets: matchedSets, Rules: matchedRules}
			if jsonOut {
				return writeJSONValue(os.Stdout, result)
			}

			w := os.Stdout
			if len(matchedSets) > 0 {
				fmt.Fprintf(w, "Rule Sets (%d):\n", len(matchedSets))
				for i := range matchedSets {
					s := &matchedSets[i]
					fmt.Fprintf(w, "  [%-8s]  %s\n", s.stateLabel(), s.DisplayName)
					fmt.Fprintf(w, "             id: %s | category: %s\n", s.ID, s.CategoryName)
					fmt.Fprintf(w, "             precise: en=%-5v al=%-5v | broad: en=%-5v al=%-5v\n",
						s.PreciseEnabled, s.PreciseAlerting, s.BroadEnabled, s.BroadAlerting)
					if d := truncate(s.Description, 100); d != "" {
						fmt.Fprintf(w, "             %s\n", d)
					}
				}
				fmt.Fprintln(w)
			}

			if len(matchedRules) > 0 {
				fmt.Fprintf(w, "Rules (%d):\n", len(matchedRules))
				for i := range matchedRules {
					r := &matchedRules[i]
					fmt.Fprintf(w, "  %-42s %-9s %-7s %-12s %s\n",
						r.ID, severityName(r.Severity), r.Precision, mitreShort(r.Tactics), r.DisplayName)
				}
				fmt.Fprintln(w)
			}

			total := len(matchedSets) + len(matchedRules)
			if total == 0 {
				fmt.Fprintln(w, "no results.")
			} else {
				fmt.Fprintf(w, "%d results (%d rule sets, %d rules)\n", total, len(matchedSets), len(matchedRules))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&installed, "installed", false, "restrict to enabled (deployed) rule sets and their rules")
	f.StringVar(&tactic, "tactic", "", "MITRE tactic id (e.g. TA0005) or name substring — applies to rules only")
	f.StringVar(&severity, "severity", "", "severity filter (e.g. High) — applies to rules only")
	return markJSON(cmd)
}

func searchRuleSets(sets []enrichedRuleSet, query string, installedOnly bool) []enrichedRuleSet {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []enrichedRuleSet
	for i := range sets {
		s := &sets[i]
		if installedOnly && !s.isEnabled() {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(s.DisplayName), q) &&
			!strings.Contains(strings.ToLower(s.Description), q) {
			continue
		}
		out = append(out, *s)
	}
	return out
}

func searchRules(rules []chronicle.CuratedRule, query, tactic, severity string, installedOnly bool, enabledSets map[string]bool) []chronicle.CuratedRule {
	q := strings.ToLower(strings.TrimSpace(query))
	tac := strings.ToLower(strings.TrimSpace(tactic))
	sev := strings.TrimSpace(severity)
	var out []chronicle.CuratedRule
	for i := range rules {
		r := &rules[i]
		if installedOnly && !enabledSets[r.RuleSetID()] {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(r.DisplayName), q) &&
			!strings.Contains(strings.ToLower(r.Description), q) {
			continue
		}
		if tac != "" && !mitreMatches(r.Tactics, tac) && !mitreMatches(r.Techniques, tac) {
			continue
		}
		if sev != "" && !strings.EqualFold(severityName(r.Severity), sev) {
			continue
		}
		out = append(out, *r)
	}
	return out
}

func enabledSetIDMap(sets []enrichedRuleSet) map[string]bool {
	m := make(map[string]bool, len(sets))
	for i := range sets {
		if sets[i].isEnabled() {
			m[sets[i].ID] = true
		}
	}
	return m
}
