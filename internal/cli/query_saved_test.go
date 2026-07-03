package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadQueryText asserts a query file strips comments and blank lines and
// trims surrounding whitespace, so a tracked .udm file can be self-documenting.
func TestReadQueryText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "q.udm")
	content := "# failed logins in the window\n\nmetadata.event_type = \"USER_LOGIN\"\n# trailing comment\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readQueryText(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := `metadata.event_type = "USER_LOGIN"`; got != want {
		t.Errorf("readQueryText = %q, want %q", got, want)
	}
}

// TestReadQueryTextMissing surfaces a not-exist error the caller can detect.
func TestReadQueryTextMissing(t *testing.T) {
	_, err := readQueryText(filepath.Join(t.TempDir(), "nope.udm"))
	if !os.IsNotExist(err) {
		t.Errorf("want not-exist error, got %v", err)
	}
}

func TestApplyParams(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		params  []string
		want    string
		wantErr bool
	}{
		{
			"no params",
			`event_type = "USER_LOGIN"`, nil,
			`event_type = "USER_LOGIN"`, false,
		},
		{
			"single",
			`principal.user.emailAddresses = "$email"`,
			[]string{"email=alice@example.com"},
			`principal.user.emailAddresses = "alice@example.com"`, false,
		},
		{
			"multiple",
			`$type AND $user`,
			[]string{"type=USER_LOGIN", "user=bob"},
			`USER_LOGIN AND bob`, false,
		},
		{"missing placeholder", `event_type = "X"`, []string{"nope=val"}, "", true},
		{"bad format", `$x`, []string{"noequals"}, "", true},
		{"empty key", `$x`, []string{"=val"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyParams(tt.filter, tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
