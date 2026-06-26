package skill

import "testing"

func TestParse(t *testing.T) {
	d := Parse()
	if d.Name != "secopsctl" {
		t.Fatalf("name = %q, want secopsctl", d.Name)
	}
	if d.Description == "" {
		t.Fatal("description is empty")
	}
	if d.Body == "" {
		t.Fatal("body is empty")
	}
	if got := d.Body[:1]; got == "-" {
		t.Fatalf("body still carries frontmatter fence: starts %q", d.Body[:4])
	}
}

func TestMarkdownEmbedded(t *testing.T) {
	if len(Markdown()) < 1000 {
		t.Fatalf("embedded SKILL.md looks too small: %d bytes", len(Markdown()))
	}
}

func TestSplitFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter("---\nname: x\n---\n# Title\nbody\n")
	if fm != "name: x" {
		t.Fatalf("fm = %q", fm)
	}
	if body != "# Title\nbody\n" {
		t.Fatalf("body = %q", body)
	}
	// No frontmatter: whole document is the body.
	if fm2, body2 := splitFrontmatter("# Title\n"); fm2 != "" || body2 != "# Title\n" {
		t.Fatalf("no-frontmatter split = (%q, %q)", fm2, body2)
	}
}
