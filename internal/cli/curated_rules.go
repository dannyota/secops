package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newCuratedRulesCmd lists the individual curated rules scoped to one rule set
// (--set required), or optionally dumps all rules with --all.
func newCuratedRulesCmd() *cobra.Command {
	var f curatedRuleFilter
	var all bool
	cmd := &cobra.Command{
		Use:   "rules --set <id> [--search Q] [--tactic T] [--severity S]",
		Short: "Read-only: list curated rules in one rule set (or --all for everything)",
		Long: "List the individual Google-managed curated rules in a specific rule set.\n" +
			"--set is required (use the id from `curated rule-sets`). Pass --all to list\n" +
			"every curated rule across all sets. Filter further with --search (name/\n" +
			"description substring), --tactic (MITRE id like TA0005 or its name), or\n" +
			"--severity. `curated rule <id>` shows one rule's full detail.\n\n" +
			"Note: only ~20% of rule sets expose individual rules via the API — the rest\n" +
			"are opaque (Google-managed, toggled at the set level only).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.set == "" && !all {
				return fmt.Errorf("specify --set <id> to list rules in one rule set, or --all for everything\n\n" +
					"  curated rules --set <id>   rules in one set (id from `curated rule-sets`)\n" +
					"  curated rules --all        all curated rules across all sets")
			}
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
			if len(rules) == 0 && f.set != "" {
				fmt.Fprintln(os.Stdout, "No individual rules exposed for this rule set.")
				fmt.Fprintln(os.Stdout, "This set is toggled at the set level only:")
				fmt.Fprintf(os.Stdout, "  curated set --category <cat> --ruleset %s --precision precise --enabled\n", shortID(f.set))
				return nil
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
	fl.BoolVar(&all, "all", false, "list all curated rules across all sets (default: requires --set)")
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
			ctx := baseContext()
			r, err := c.GetCuratedRule(ctx, args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, r)
			}
			nameMap := resolveSetCategoryNames(ctx, c)
			printCuratedRuleDetail(os.Stdout, r, nameMap)
			return nil
		},
	}
	return markJSON(cmd)
}

// resolveSetCategoryNames builds a map of ruleSetID → displayName and
// categoryID → displayName for human-readable output. Two API calls
// (categories + wildcard rule-set list). Best-effort: a failure returns
// an empty map (the detail still prints with raw IDs).
func resolveSetCategoryNames(ctx context.Context, c *chronicle.Client) map[string]string {
	m := map[string]string{}
	cats, err := c.ListCuratedRuleSetCategories(ctx)
	if err != nil {
		return m
	}
	for _, cat := range cats {
		m[lastSegment(cat.Name)] = cat.DisplayName
	}
	sets, err := c.ListCuratedRuleSets(ctx, "-")
	if err != nil {
		return m
	}
	for _, s := range sets {
		m[lastSegment(s.Name)] = s.DisplayName
	}
	return m
}

// newCuratedRuleSetsCmd lists curated rule sets with deployment state. Default:
// enabled (installed) only; --all for the full catalog.
func newCuratedRuleSetsCmd() *cobra.Command {
	var (
		all      bool
		category string
		search   string
	)
	cmd := &cobra.Command{
		Use:   "rule-sets [--all] [--category C] [--search Q]",
		Short: "Read-only: list curated rule sets with deployment state (default: enabled only)",
		Long: "List the Google-managed curated rule sets with their deployment state\n" +
			"(enabled/alerting per precision). Default: shows only enabled (installed)\n" +
			"rule sets. Use --all to see the full catalog including disabled sets.\n\n" +
			"  curated rule-sets                          enabled sets (what you're running)\n" +
			"  curated rule-sets --all                    full catalog\n" +
			"  curated rule-sets --category cloud         sets in one category (name or id)\n" +
			"  curated rule-sets --search azure           text search on name/description\n" +
			"  curated rule-sets --all --search aws       search the full catalog\n\n" +
			"Use `curated rules --set <id>` to drill into a set's individual rules.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			sets, _, err := loadEnrichedRuleSets(baseContext(), c)
			if err != nil {
				return err
			}

			sets = filterEnrichedSets(sets, all, category, search)

			if jsonOut {
				return writeJSONValue(os.Stdout, sets)
			}
			return emitEnrichedSets(os.Stdout, sets, all)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&all, "all", false, "show all rule sets including disabled (default: enabled only)")
	f.StringVar(&category, "category", "", "limit to one category (display name substring or UUID)")
	f.StringVar(&search, "search", "", "case-insensitive substring match on set name or description")
	return markJSON(cmd)
}

func filterEnrichedSets(sets []enrichedRuleSet, all bool, category, search string) []enrichedRuleSet {
	cat := strings.TrimSpace(category)
	q := strings.ToLower(strings.TrimSpace(search))
	var out []enrichedRuleSet
	for i := range sets {
		s := &sets[i]
		if !all && !s.isEnabled() {
			continue
		}
		if cat != "" && !matchCategory(s.CategoryID, s.CategoryName, cat) {
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

func emitEnrichedSets(w io.Writer, sets []enrichedRuleSet, showAll bool) error {
	if len(sets) == 0 {
		fmt.Fprintln(w, "no matching rule sets.")
		return nil
	}
	for i := range sets {
		s := &sets[i]
		fmt.Fprintf(w, "[%-8s]  %s\n", s.stateLabel(), s.DisplayName)
		fmt.Fprintf(w, "           id: %s | category: %s\n", s.ID, s.CategoryName)
		fmt.Fprintf(w, "           precise: en=%-5v al=%-5v | broad: en=%-5v al=%-5v\n",
			s.PreciseEnabled, s.PreciseAlerting, s.BroadEnabled, s.BroadAlerting)
	}
	label := "enabled"
	if showAll {
		label = "total"
	}
	fmt.Fprintf(w, "\n%d %s rule set(s)\n", len(sets), label)
	return nil
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

// printCuratedRuleDetail renders one curated rule's metadata. nameMap maps
// set/category IDs to display names (best-effort, may be empty).
func printCuratedRuleDetail(w io.Writer, r *chronicle.CuratedRule, nameMap map[string]string) {
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
		setID := r.RuleSetID()
		catID := r.CategoryID()
		setLabel := resolvedLabel(setID, nameMap)
		catLabel := resolvedLabel(catID, nameMap)
		fmt.Fprintf(w, "  rule set:    %s\n", setLabel)
		fmt.Fprintf(w, "  category:    %s\n", catLabel)
	}
	if r.UpdateTime != "" {
		fmt.Fprintf(w, "  updated:     %s\n", r.UpdateTime)
	}
	if d := strings.TrimSpace(r.Description); d != "" {
		fmt.Fprintf(w, "  description: %s\n", d)
	}
	fmt.Fprintln(w, "  (Google-managed — YARA-L source is not exposed by the API)")
}

// resolvedLabel returns "DisplayName (id)" if the name is known, else just id.
func resolvedLabel(id string, nameMap map[string]string) string {
	if name, ok := nameMap[id]; ok && name != "" {
		return fmt.Sprintf("%s (%s)", name, id)
	}
	return id
}
