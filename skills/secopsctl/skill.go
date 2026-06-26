// Package skill embeds the secopsctl agent operating guide (SKILL.md) so the
// compiled binary can hand an agent its own usage doc. An install via
// `go install danny.vn/secops/cmd/secopsctl@latest` ships only the binary, not
// the repo's skills/ tree, so the guide has to travel inside the binary.
package skill

import (
	_ "embed"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed SKILL.md
var markdown string

// Markdown returns the full SKILL.md, frontmatter included.
func Markdown() string { return markdown }

// Doc is the parsed guide: frontmatter metadata plus the markdown body.
type Doc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// Parse splits the embedded SKILL.md into its frontmatter metadata and body.
func Parse() Doc {
	fm, body := splitFrontmatter(markdown)
	var f struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	_ = yaml.Unmarshal([]byte(fm), &f)
	return Doc{
		Name:        strings.TrimSpace(f.Name),
		Description: strings.TrimSpace(f.Description),
		Body:        body,
	}
}

// splitFrontmatter returns the YAML frontmatter (without the `---` fences) and the
// markdown body. With no leading frontmatter, fm is empty and body is the whole doc.
func splitFrontmatter(s string) (fm, body string) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s
	}
	before, after, found := strings.Cut(s[len("---\n"):], "\n---")
	if !found {
		return "", s
	}
	return before, strings.TrimLeft(after, "\n")
}
