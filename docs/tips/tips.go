// Package tips embeds the operator guides so the compiled binary can serve
// them — as parser-extension tips (`ingest parsers extension tips`) and as MCP
// resources (`mcp serve`). An install via `go install` ships only the binary,
// not the repo's docs/ tree, so the guides must travel inside the binary.
package tips

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed *.md
var allFiles embed.FS

//go:embed 12-parser-extensions.md
var parserExtensions string

// ParserExtensions returns the parser-extension authoring guide (Markdown).
func ParserExtensions() string { return parserExtensions }

// Entry is one embedded tip file: its base name and first-line summary.
type Entry struct {
	Name    string // base filename without extension, e.g. "15-recipes"
	Title   string // first heading text, e.g. "Recipes — cross-cutting workflows"
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
