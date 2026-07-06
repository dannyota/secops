package cli

import (
	"strings"
	"testing"
)

func TestAuditUserQueryBuilder(t *testing.T) {
	email := "alice@example.com"
	tests := []struct {
		category string
		wantSub  []string
	}{
		{"login", []string{`USER_LOGIN`, email}},
		{"admin", []string{`USER_CHANGE_PERMISSIONS`, `USER_CHANGE_PASSWORD`, email}},
		{"password", []string{`USER_CHANGE_PASSWORD`, `principal.user.emailAddresses`, `target.user.emailAddresses`, email}},
		{"oauth", []string{`USER_RESOURCE_ACCESS`, `target.application`, email}},
		{"iam", []string{`USER_CHANGE_PERMISSIONS`, `RESOURCE_PERMISSIONS_CHANGE`, email}},
		{"resource", []string{`RESOURCE_READ`, `RESOURCE_WRITTEN`, email}},
	}
	for _, tt := range tests {
		q := auditUserQueryForCategory(tt.category, email)
		if q == "" {
			t.Errorf("category %q: got empty query", tt.category)
			continue
		}
		for _, sub := range tt.wantSub {
			if !strings.Contains(q, sub) {
				t.Errorf("category %q: query missing %q\n  got: %s", tt.category, sub, q)
			}
		}
	}
}

func TestAuditUserQueryUnknownCategory(t *testing.T) {
	if q := auditUserQueryForCategory("bogus", "x@y.com"); q != "" {
		t.Errorf("unknown category should return empty, got %q", q)
	}
}

func TestFilterCategories(t *testing.T) {
	all := filterCategories("")
	if len(all) != 6 {
		t.Errorf("empty selector: got %d categories, want 6", len(all))
	}

	two := filterCategories("login,resource")
	if len(two) != 2 {
		t.Errorf("login,resource: got %d categories, want 2", len(two))
	}
	if two[0].Name != "login" || two[1].Name != "resource" {
		t.Errorf("login,resource: got [%s, %s]", two[0].Name, two[1].Name)
	}

	none := filterCategories("bogus")
	if len(none) != 0 {
		t.Errorf("bogus: got %d categories, want 0", len(none))
	}
}

func TestAuditUserRejectsNonEmail(t *testing.T) {
	cmd := newAuditUserCmd()
	cmd.SetArgs([]string{"notanemail"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "email address") {
		t.Errorf("want email validation error, got %v", err)
	}
}
