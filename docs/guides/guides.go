// Package guides embeds the user-facing guide Markdown so the compiled binary
// can serve them as MCP resources. An install via `go install` ships only the
// binary, not the repo's docs/ tree, so the guides must travel inside it.
package guides

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed *.md
var allFiles embed.FS

// Entry is one embedded guide file.
type Entry struct {
	Name    string // base filename without extension, e.g. "search"
	Title   string // first heading text
	Content string // full Markdown content
}

// All returns every embedded *.md file as an Entry, sorted by filename.
func All() []Entry {
	var entries []Entry
	_ = fs.WalkDir(allFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := allFiles.ReadFile(path)
		if err != nil {
			return nil
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		title := extractTitle(string(data))
		entries = append(entries, Entry{Name: name, Title: title, Content: string(data)})
		return nil
	})
	return entries
}

func extractTitle(md string) string {
	for _, line := range strings.SplitN(md, "\n", 10) {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}
