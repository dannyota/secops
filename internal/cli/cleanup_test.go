package cli

import "testing"

func TestIsSecopsctlSmokeName(t *testing.T) {
	for _, s := range []string{
		"secopsctl-smoketest-rule-123",
		"secopsctl_smoketest_rule_123",
		"secopsctl-smoke-123",
		"secopsctl_smoke_reflist",
	} {
		if !isSecopsctlSmokeName(s) {
			t.Fatalf("%q was not treated as a smoke name", s)
		}
	}
	for _, s := range []string{
		"prod-secopsctl-smoketest-rule",
		"secopsctl",
		"smoketest-secopsctl-rule",
		"",
	} {
		if isSecopsctlSmokeName(s) {
			t.Fatalf("%q should not be treated as a smoke name", s)
		}
	}
}
