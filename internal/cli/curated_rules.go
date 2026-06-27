package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Read surfaces over Google-managed curated content: list/search the individual
// curated rules, view one rule's detail, and list the curated rule SETS. Curated
// rules are Google-managed (no source code via the API); an operator toggles them
// at the rule-set deployment level (`curated set`).

// newCuratedRulesCmd lists/searches the individual curated rules, filterable by
// search term, parent set, category, MITRE tactic, or severity.
func newCuratedRulesCmd() *cobra.Command {
	var f curatedRuleFilter
	cmd := &cobra.Command{
		Use:   "rules [--search Q] [--set ID] [--category ID] [--tactic T] [--severity S]",
		Short: "Read-only: list/search the individual curated (Google-managed) rules",
		Long: "List the individual Google-managed curated rules (distinct from the rule SETS\n" +
			"that `rule-sets`/`list` show). Filter client-side: --search (rule name or\n" +
			"description substring), --set (only rules in one rule set — the id from\n" +
			"`curated rule-sets`), --category, --tactic (MITRE id like TA0005 or its name),\n" +
			"or --severity. `curated rule <id>` shows one rule's full detail.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rules, err := c.ListCuratedRules(baseContext())
			if err != nil {
				return err
			}
			rules = filterCuratedRules(rules, f)
			if jsonOut {
				return writeJSONValue(os.Stdout, rules)
			}
			for i := range rules {
				r := &rules[i]
				fmt.Fprintf(os.Stdout, "%-42s %-9s %-7s %-12s %s\n",
					r.ID, severityName(r.Severity), r.Precision, mitreShort(r.Tactics), r.DisplayName)
			}
			fmt.Fprintf(os.Stdout, "\n%d curated rule(s)\n", len(rules))
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.search, "search", "", "case-insensitive substring match on rule name or description")
	fl.StringVar(&f.set, "set", "", "only rules in this curated rule set (id from `curated rule-sets`)")
	fl.StringVar(&f.category, "category", "", "only rules in this category id")
	fl.StringVar(&f.tactic, "tactic", "", "only rules with this MITRE tactic (id like TA0005, or its name)")
	fl.StringVar(&f.severity, "severity", "", "only rules with this severity (e.g. High)")
	return markJSON(cmd)
}

// newCuratedRuleCmd shows one curated rule's full detail.
func newCuratedRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule <curated-rule-id>",
		Short: "Read-only: view one curated rule's detail (metadata, MITRE, parent set, description)",
		Long: "Show a single Google-managed curated rule: severity, type, precision, MITRE\n" +
			"tactics/techniques, parent rule set, and description. Curated rules are\n" +
			"Google-managed, so the YARA-L source is not exposed by the API — this is the\n" +
			"metadata the console's rule view shows. The id is a `ur_...` from `curated rules`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			r, err := c.GetCuratedRule(baseContext(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, r)
			}
			printCuratedRuleDetail(os.Stdout, r)
			return nil
		},
	}
	return markJSON(cmd)
}

// newCuratedRuleSetsCmd lists the curated rule SETS (the groupings a `curated set`
// toggle acts on); pair with `curated rules --set <id>` to see a set's rules.
func newCuratedRuleSetsCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "rule-sets [--category ID]",
		Short: "Read-only: list curated rule SETS (id · severity · precisions · name)",
		Long: "List the Google-managed curated rule sets — the groupings a deployment toggle\n" +
			"(`curated set`) acts on, and what `list` shows deployment state for. Use a set\n" +
			"id with `curated rules --set <id>` to see the rules in that set. --category\n" +
			"limits to one category id (default: all categories).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			cat := strings.TrimSpace(category)
			if cat == "" {
				cat = "-" // wildcard: every category
			}
			sets, err := c.ListCuratedRuleSets(baseContext(), cat)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, sets)
			}
			for i := range sets {
				s := &sets[i]
				fmt.Fprintf(os.Stdout, "%-40s %-9s %-14s %s\n",
					shortID(s.Name), severityName(s.Severity), strings.Join(s.Precisions, ","), s.DisplayName)
			}
			fmt.Fprintf(os.Stdout, "\n%d curated rule set(s)\n", len(sets))
			return nil
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "limit to one category id (default: all)")
	return markJSON(cmd)
}

// curatedRuleFilter holds the client-side filters for `curated rules`.
type curatedRuleFilter struct{ search, set, category, tactic, severity string }

// filterCuratedRules applies the (all-AND) client-side filters. An empty filter
// field is a no-op.
func filterCuratedRules(rules []chronicle.CuratedRule, f curatedRuleFilter) []chronicle.CuratedRule {
	search := strings.ToLower(strings.TrimSpace(f.search))
	tactic := strings.ToLower(strings.TrimSpace(f.tactic))
	sev := strings.TrimSpace(f.severity)
	set := shortID(strings.TrimSpace(f.set))
	cat := strings.TrimSpace(f.category)
	var out []chronicle.CuratedRule
	for i := range rules {
		r := &rules[i]
		if search != "" && !strings.Contains(strings.ToLower(r.DisplayName), search) &&
			!strings.Contains(strings.ToLower(r.Description), search) {
			continue
		}
		if set != "" && !strings.EqualFold(r.RuleSetID(), set) {
			continue
		}
		if cat != "" && !strings.EqualFold(r.CategoryID(), cat) {
			continue
		}
		if sev != "" && !strings.EqualFold(severityName(r.Severity), sev) {
			continue
		}
		if tactic != "" && !mitreMatches(r.Tactics, tactic) && !mitreMatches(r.Techniques, tactic) {
			continue
		}
		out = append(out, *r)
	}
	return out
}

// mitreMatches reports whether any ref's id or display name contains q (lowercased).
func mitreMatches(refs []chronicle.MitreRef, q string) bool {
	for _, m := range refs {
		if strings.Contains(strings.ToLower(m.ID), q) || strings.Contains(strings.ToLower(m.DisplayName), q) {
			return true
		}
	}
	return false
}

// mitreShort joins the refs' ids for a compact column, or "—" when none.
func mitreShort(refs []chronicle.MitreRef) string {
	if len(refs) == 0 {
		return "—"
	}
	ids := make([]string, 0, len(refs))
	for _, m := range refs {
		ids = append(ids, m.ID)
	}
	return strings.Join(ids, ",")
}

// severityName returns a severity's display name, or "" when unset.
func severityName(s *chronicle.Severity) string {
	if s == nil {
		return ""
	}
	return s.DisplayName
}

// shortID returns the last path segment of a resource name (or the input when it
// carries no "/"), so a flag accepts either a short id or a full resource name.
func shortID(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// printCuratedRuleDetail renders one curated rule's metadata. Curated rules are
// Google-managed; the API exposes no source, so the YARA-L is deliberately absent.
func printCuratedRuleDetail(w io.Writer, r *chronicle.CuratedRule) {
	fmt.Fprintf(w, "%s\n", r.DisplayName)
	fmt.Fprintf(w, "  id:          %s\n", r.ID)
	if sev := severityName(r.Severity); sev != "" {
		fmt.Fprintf(w, "  severity:    %s\n", sev)
	}
	if t := r.Type; t != "" {
		fmt.Fprintf(w, "  type:        %s\n", t)
	}
	if r.Precision != "" {
		fmt.Fprintf(w, "  precision:   %s\n", r.Precision)
	}
	for _, m := range r.Tactics {
		fmt.Fprintf(w, "  tactic:      %s %s\n", m.ID, m.DisplayName)
	}
	for _, m := range r.Techniques {
		fmt.Fprintf(w, "  technique:   %s %s\n", m.ID, m.DisplayName)
	}
	if r.CuratedRuleSet != "" {
		fmt.Fprintf(w, "  rule set:    %s (category %s)\n", r.RuleSetID(), r.CategoryID())
	}
	if r.UpdateTime != "" {
		fmt.Fprintf(w, "  updated:     %s\n", r.UpdateTime)
	}
	if d := strings.TrimSpace(r.Description); d != "" {
		fmt.Fprintf(w, "  description: %s\n", d)
	}
	fmt.Fprintln(w, "  (Google-managed — YARA-L source is not exposed by the API)")
}
