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

// TestValidSavedQueryName rejects names that could escape the pack directory.
func TestValidSavedQueryName(t *testing.T) {
	bad := []string{"", ".", "..", "../secret", "a/b", `a\b`, "..\\x", "x/../y"}
	for _, n := range bad {
		if err := validSavedQueryName(n); err == nil {
			t.Errorf("validSavedQueryName(%q) = nil, want error", n)
		}
	}
	for _, n := range []string{"failed-logins", "my_query", "q1.thing"} {
		if err := validSavedQueryName(n); err != nil {
			t.Errorf("validSavedQueryName(%q) = %v, want nil", n, err)
		}
	}
}
