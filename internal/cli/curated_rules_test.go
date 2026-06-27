package cli

import (
	"testing"

	"danny.vn/secops/chronicle"
)

func curatedTestRules() []chronicle.CuratedRule {
	base := "projects/p/locations/l/instances/i/curatedRuleSetCategories/"
	mk := func(id, cat, set, name, desc, sev string, tactics ...string) chronicle.CuratedRule {
		r := chronicle.CuratedRule{
			DisplayName:    name,
			Description:    desc,
			CuratedRuleSet: base + cat + "/curatedRuleSets/" + set,
		}
		if sev != "" {
			r.Severity = &chronicle.Severity{DisplayName: sev}
		}
		for _, t := range tactics {
			r.Tactics = append(r.Tactics, chronicle.MitreRef{ID: t, DisplayName: "Defense Evasion"})
		}
		return r
	}
	return []chronicle.CuratedRule{
		mk("ur_a", "cat1", "set1", "Gcp Firewall Rule Created", "creates a firewall", "Low", "TA0005"),
		mk("ur_b", "cat1", "set2", "Linux Persistence", "startup folder write", "High", "TA0003"),
		mk("ur_c", "cat2", "set3", "Azure Network Flow", "vnet change", "Info", "TA0005"),
	}
}

func TestFilterCuratedRules(t *testing.T) {
	rules := curatedTestRules()
	cases := []struct {
		name string
		f    curatedRuleFilter
		want int
	}{
		{"no filter", curatedRuleFilter{}, 3},
		{"search name", curatedRuleFilter{search: "firewall"}, 1},
		{"search description", curatedRuleFilter{search: "startup"}, 1},
		{"set short id", curatedRuleFilter{set: "set1"}, 1},
		{"set full name", curatedRuleFilter{set: "projects/p/locations/l/instances/i/curatedRuleSetCategories/cat1/curatedRuleSets/set2"}, 1},
		{"category", curatedRuleFilter{category: "cat1"}, 2},
		{"severity", curatedRuleFilter{severity: "high"}, 1},
		{"tactic by id", curatedRuleFilter{tactic: "TA0005"}, 2},
		{"tactic by name", curatedRuleFilter{tactic: "defense"}, 3},
		{"combined (none match)", curatedRuleFilter{search: "firewall", severity: "High"}, 0},
	}
	for _, tc := range cases {
		if got := len(filterCuratedRules(rules, tc.f)); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestCuratedRuleSetMembershipParsing(t *testing.T) {
	r := curatedTestRules()[0] // cat1/set1
	if got := r.RuleSetID(); got != "set1" {
		t.Errorf("RuleSetID = %q, want set1", got)
	}
	if got := r.CategoryID(); got != "cat1" {
		t.Errorf("CategoryID = %q, want cat1", got)
	}
	var empty chronicle.CuratedRule
	if empty.RuleSetID() != "" || empty.CategoryID() != "" {
		t.Errorf("empty rule should yield empty set/category ids")
	}
}
