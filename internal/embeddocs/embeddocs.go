// Package embeddocs walks an embedded Markdown tree into (name, title,
// content) entries — the shared machinery behind the docs/tips and
// docs/guides embed packages.
package embeddocs

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Entry is one embedded Markdown file.
type Entry struct {
	Name    string // base filename without extension, e.g. "search"
	Title   string // first heading text
	Content string // full Markdown content
}

// All returns every *.md file in files as an Entry, in fs.WalkDir order
// (lexical by filename).
func All(files fs.FS) []Entry {
	var entries []Entry
	_ = fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return nil
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		entries = append(entries, Entry{Name: name, Title: extractTitle(string(data)), Content: string(data)})
		return nil
	})
	return entries
}

// extractTitle returns the first `# ` heading within the leading lines.
func extractTitle(md string) string {
	for _, line := range strings.SplitN(md, "\n", 10) {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}
