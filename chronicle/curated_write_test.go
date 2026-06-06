package chronicle

import "testing"

func TestValidateCuratedPrecision(t *testing.T) {
	for _, ok := range []string{"precise", "broad"} {
		if err := validateCuratedPrecision(ok); err != nil {
			t.Errorf("precision %q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "PRECISE", "medium", "high"} {
		if err := validateCuratedPrecision(bad); err == nil {
			t.Errorf("precision %q accepted, want error", bad)
		}
	}
}

func TestCuratedDeploymentPath(t *testing.T) {
	got := curatedDeploymentPath("cat1", "rs1", "precise")
	want := "curatedRuleSetCategories/cat1/curatedRuleSets/rs1/curatedRuleSetDeployments/precise"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestParseCuratedDeploymentName(t *testing.T) {
	full := "projects/p/locations/r/instances/c/curatedRuleSetCategories/CAT/curatedRuleSets/RS/curatedRuleSetDeployments/broad"
	cat, rs, prec, err := ParseCuratedDeploymentName(full)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cat != "CAT" || rs != "RS" || prec != "broad" {
		t.Errorf("got (%q,%q,%q), want (CAT,RS,broad)", cat, rs, prec)
	}

	// Relative form also works.
	cat, rs, prec, err = ParseCuratedDeploymentName(curatedDeploymentPath("c2", "r2", "precise"))
	if err != nil || cat != "c2" || rs != "r2" || prec != "precise" {
		t.Errorf("relative parse: (%q,%q,%q) err=%v", cat, rs, prec, err)
	}

	if _, _, _, err := ParseCuratedDeploymentName("rules/abc/deployment"); err == nil {
		t.Error("non-curated name should error")
	}
}
