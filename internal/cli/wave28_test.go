package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestReadIndicatorList: file input skips blanks and # comments, trims whitespace.
func TestReadIndicatorList(t *testing.T) {
	p := filepath.Join(t.TempDir(), "iocs.txt")
	if err := os.WriteFile(p, []byte("a.com\n\n# a comment\n  1.2.3.4  \nevil.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readIndicatorList(&cobra.Command{}, p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.com", "1.2.3.4", "evil.example"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readIndicatorList = %v, want %v", got, want)
	}
}

// TestReadIndicatorListStdin: "-" reads from the command's stdin.
func TestReadIndicatorListStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("x.com\n# c\nhash123\n"))
	got, err := readIndicatorList(cmd, "-")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"x.com", "hash123"}) {
		t.Errorf("stdin read = %v", got)
	}
}

// TestPullHasForwarders: forwarders is a pull target (symmetry with push/drift).
func TestPullHasForwarders(t *testing.T) {
	found := false
	for _, p := range pullOrder {
		if p.name == "forwarders" {
			found = true
		}
	}
	if !found {
		t.Error("forwarders is not a pull target")
	}
}
