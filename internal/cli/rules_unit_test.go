package cli

import (
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestToRuleToken(t *testing.T) {
	cases := map[string]string{
		"My New Rule": "My_New_Rule",
		"a-b.c":       "a_b_c",
		"123abc":      "r_123abc",
		"  _x_  ":     "x",
		"":            "",
		"   ":         "",
	}
	for in, want := range cases {
		if got := toRuleToken(in); got != want {
			t.Errorf("toRuleToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteRuleToken(t *testing.T) {
	src := "rule foo {\n  meta:\n    author = \"x\"\n  condition:\n    $e\n}\n"
	out, old := rewriteRuleToken(src, "bar")
	if old != "foo" {
		t.Errorf("old token = %q, want foo", old)
	}
	if !strings.Contains(out, "rule bar {") || strings.Contains(out, "rule foo {") {
		t.Errorf("rewrite failed:\n%s", out)
	}
	// Probe mode (empty newToken) returns the original text + the token.
	if probed, tok := rewriteRuleToken(src, ""); probed != src || tok != "foo" {
		t.Errorf("probe mode changed text or token (%q)", tok)
	}
	// No declaration → empty token, text unchanged.
	if _, tok := rewriteRuleToken("not a rule", "bar"); tok != "" {
		t.Errorf("expected empty token for non-rule text, got %q", tok)
	}
}

func TestUnifiedDiff(t *testing.T) {
	d := unifiedDiff("x\ny\nz", "x\nQ\nz", "a", "b")
	for _, want := range []string{"--- a", "+++ b", "  x", "- y", "+ Q", "  z"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing %q:\n%s", want, d)
		}
	}
	// Identical inputs → no change lines (a change line begins with "\n- "/"\n+ ";
	// the "--- a"/"+++ b" headers must not trip this).
	if d := unifiedDiff("same", "same", "a", "b"); strings.Contains(d, "\n- ") || strings.Contains(d, "\n+ ") {
		t.Errorf("identical inputs produced changes:\n%s", d)
	}
}

func TestMitreAgg(t *testing.T) {
	a := newMitreAgg()
	a.add("ru_1", []string{"TA0002"}, []chronicle.MitreRef{{ID: "T1059"}})
	a.add("ru_2", []string{"TA0002"}, []chronicle.MitreRef{{ID: "T1059"}}) // same technique, 2nd rule
	a.add("ur_1", []string{"TA0005"}, []chronicle.MitreRef{{ID: "T1003", DisplayName: "OS Cred Dumping"}})
	a.add("ru_3", nil, nil) // unmapped

	rows := a.rows()
	if rows[0].Technique != "T1059" || rows[0].RuleCount != 2 {
		t.Errorf("expected T1059 first with 2 rules, got %+v", rows[0])
	}
	if last := rows[len(rows)-1]; last.Technique != mitreUnmapped || last.RuleCount != 1 {
		t.Errorf("expected UNMAPPED last with 1 rule, got %+v", last)
	}
	s := a.summary(rows)
	if s["rules_total"] != 4 || s["custom"] != 3 || s["curated"] != 1 {
		t.Errorf("summary counts wrong: %v", s)
	}
	if s["techniques_covered"] != 2 || s["rules_unmapped"] != 1 {
		t.Errorf("coverage summary wrong: %v", s)
	}
}

func TestClassifyHealth(t *testing.T) {
	cases := []struct {
		name string
		row  healthRow
		want string
	}{
		{"compile-fail", healthRow{Compile: "FAILED"}, healthFailing},
		{"exec-limited", healthRow{Compile: "SUCCEEDED", Execution: "LIMITED"}, healthErroring},
		{"silent", healthRow{Compile: "SUCCEEDED", Enabled: true, Alerting: true, Detections: 0}, healthSilent},
		{"healthy-active", healthRow{Compile: "SUCCEEDED", Enabled: true, Alerting: true, Detections: 5}, healthHealthy},
		{"healthy-disabled", healthRow{Compile: "SUCCEEDED", Enabled: false}, healthHealthy},
	}
	for _, tc := range cases {
		if got := classifyHealth(tc.row); got != tc.want {
			t.Errorf("%s: classifyHealth = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestReframeRuleErr(t *testing.T) {
	hinted := reframeRuleErr(`parsing: error with token: "#"`)
	if !strings.Contains(hinted, "count($e.metadata.id)") || !strings.Contains(hinted, "condition:") {
		t.Errorf("missing #-token hint: %q", hinted)
	}
	plain := "unknown field: principal.bogus"
	if got := reframeRuleErr(plain); got != plain {
		t.Errorf("unrelated message must pass through verbatim, got %q", got)
	}
}

func TestDiagPosition(t *testing.T) {
	cases := []struct {
		name string
		d    chronicle.RuleDiagnostic
		want string
	}{
		{"none", chronicle.RuleDiagnostic{}, ""},
		{"line only", chronicle.RuleDiagnostic{Position: map[string]int{"startLine": 12}}, " (line 12)"},
		{"line and col", chronicle.RuleDiagnostic{Position: map[string]int{"startLine": 12, "startColumn": 5}}, " (line 12, col 5)"},
	}
	for _, tc := range cases {
		if got := diagPosition(tc.d); got != tc.want {
			t.Errorf("%s: diagPosition = %q, want %q", tc.name, got, tc.want)
		}
	}
}
