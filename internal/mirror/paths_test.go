package mirror

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	if got := Slugify("My Rule Name"); got != "My_Rule_Name" {
		t.Errorf("Slugify(%q) = %q, want %q", "My Rule Name", got, "My_Rule_Name")
	}

	// Idempotent: slugging a slug is a no-op.
	once := Slugify("Some Mixed Name")
	if twice := Slugify(once); once != twice {
		t.Errorf("Slugify not idempotent: %q -> %q", once, twice)
	}

	// Unsafe characters never survive.
	got := Slugify(`a/b\c:d*e?f`)
	for _, bad := range []string{"/", `\`, ":", "*", "?"} {
		if strings.Contains(got, bad) {
			t.Errorf("Slugify left unsafe char %q in %q", bad, got)
		}
	}

	// Empty / all-unsafe input yields the sentinel, never "".
	for _, in := range []string{"", "   ", "!!!", "///"} {
		if got := Slugify(in); got != "_unnamed" {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, "_unnamed")
		}
	}
}

func TestLastSegment(t *testing.T) {
	cases := map[string]string{
		"projects/p/locations/us/instances/i/rules/ru_123": "ru_123",
		"no-slashes": "no-slashes",
		"trailing/":  "",
	}
	for in, want := range cases {
		if got := lastSegment(in); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}
