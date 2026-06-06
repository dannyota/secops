package mirror

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameRuleText(t *testing.T) {
	if !sameRuleText("rule x {}\n", "rule x {}") {
		t.Error("trailing-newline-only difference should compare equal")
	}
	if !sameRuleText("a\nb\n", "a\nb\n") {
		t.Error("identical text should compare equal")
	}
	if sameRuleText("rule x {}", "rule y {}") {
		t.Error("a real text change must be detected")
	}
}

func TestCompanionPath(t *testing.T) {
	if got := companionPath("/d/foo.yaral"); got != "/d/foo.yaml" {
		t.Errorf("companionPath = %q, want /d/foo.yaml", got)
	}
}

func TestDeployTriple(t *testing.T) {
	if got := deployTriple(true, false, ""); got != "en=true al=false -" {
		t.Errorf("deployTriple empty freq = %q", got)
	}
	if got := deployTriple(false, true, "HOURLY"); got != "en=false al=true HOURLY" {
		t.Errorf("deployTriple = %q", got)
	}
}

func TestTrackedRules(t *testing.T) {
	dir := t.TempDir()
	// A tracked rule: companion + sibling .yaral.
	comp := ruleCompanion{DisplayName: "My Rule", RuleID: "ru_1", Etag: "e1"}
	if err := comp.write(filepath.Join(dir, "my_rule.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my_rule.yaral"), []byte("rule my_rule {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A stray .yaral with no companion is NOT tracked (rules-create territory).
	if err := os.WriteFile(filepath.Join(dir, "new.yaral"), []byte("rule new {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := trackedRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("trackedRules = %d, want 1", len(got))
	}
	if got[0].comp.RuleID != "ru_1" || got[0].yaral != filepath.Join(dir, "my_rule.yaral") {
		t.Errorf("tracked rule wrong: %+v (yaral=%s)", got[0].comp, got[0].yaral)
	}

	// Missing dir → no rules, no error.
	if rs, err := trackedRules(filepath.Join(dir, "nope")); err != nil || rs != nil {
		t.Errorf("missing dir: rs=%v err=%v", rs, err)
	}
}
