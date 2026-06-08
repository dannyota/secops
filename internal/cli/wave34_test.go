package cli

import (
	"regexp"
	"strings"
	"testing"

	"danny.vn/secops/soar/legacy"
)

// TestLegacyOpIndexLoads locks that the bundled op index decodes, is non-trivial,
// and every entry is well-formed (leading-slash op + a known HTTP method).
func TestLegacyOpIndexLoads(t *testing.T) {
	ops, err := loadLegacyOps()
	if err != nil {
		t.Fatalf("loadLegacyOps: %v", err)
	}
	if len(ops) < 400 {
		t.Fatalf("op index has %d ops, expected the full external surface (>400)", len(ops))
	}
	methods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true}
	for _, o := range ops {
		if !strings.HasPrefix(o.Op, "/") {
			t.Errorf("op %q does not start with /", o.Op)
		}
		if !methods[o.Method] {
			t.Errorf("op %q has unknown method %q", o.Op, o.Method)
		}
	}
}

// TestLegacyOpIndexTenantNeutral guards that the SHIPPED op index carries no
// id-like tenant identifiers or leaked endpoints — it is a public, committed asset
// (House Rule 3). The repo-wide pre-commit denylist hook covers named tenants.
func TestLegacyOpIndexTenantNeutral(t *testing.T) {
	ops, err := loadLegacyOps()
	if err != nil {
		t.Fatal(err)
	}
	// A run of 8+ digits would be a customer/project id; a googleapis / soar host
	// would be a leaked endpoint.
	bad := regexp.MustCompile(`(?i)\d{8,}|googleapis\.com|siemplify-soar\.com`)
	for _, o := range ops {
		if bad.MatchString(o.Op + " " + o.Summary) {
			t.Errorf("op index entry looks tenant-specific: %q / %q", o.Op, o.Summary)
		}
	}
}

// TestPrettyPriority locks the modern-priority label rendering.
func TestPrettyPriority(t *testing.T) {
	for in, want := range map[string]string{
		"PRIORITY_HIGH":      "High",
		"CRITICAL":           "Critical",
		"PRIORITY_VERY_HIGH": "Very high",
		"":                   "-",
	} {
		if got := prettyPriority(in); got != want {
			t.Errorf("prettyPriority(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDashIfEmpty locks the empty-cell rendering used across the SOAR tables.
func TestDashIfEmpty(t *testing.T) {
	if dashIfEmpty("") != "-" {
		t.Error("empty should render as -")
	}
	if dashIfEmpty("x") != "x" {
		t.Error("non-empty should pass through")
	}
}

// TestFilterUsers locks disabled-account hiding (unless --all) and the grep.
func TestFilterUsers(t *testing.T) {
	users := []legacy.UserProfile{
		{UserName: "alice", FirstName: "Alice", Email: "alice@example.com"},
		{UserName: "bob", FirstName: "Bob", Email: "bob@example.com", IsDisabled: true},
		{UserName: "carol", FirstName: "Carol", Email: "carol@example.com"},
	}
	// Disabled hidden by default.
	if got := filterUsers(users, "", false); len(got) != 2 {
		t.Errorf("default filter kept %d, want 2 (disabled hidden)", len(got))
	}
	// --all includes disabled.
	if got := filterUsers(users, "", true); len(got) != 3 {
		t.Errorf("--all filter kept %d, want 3", len(got))
	}
	// grep matches username/name/email, case-insensitive.
	if got := filterUsers(users, "CAROL", false); len(got) != 1 || got[0].UserName != "carol" {
		t.Errorf("grep carol = %+v, want [carol]", got)
	}
	// grep + disabled: bob is disabled, so hidden even on a name match without --all.
	if got := filterUsers(users, "bob", false); len(got) != 0 {
		t.Errorf("grep bob (disabled, no --all) kept %d, want 0", len(got))
	}
}
