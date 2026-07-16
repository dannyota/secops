// Package tips embeds the operator guides so the compiled binary can serve
// them — as parser-extension tips (`ingest parsers extension tips`) and as MCP
// resources (`mcp serve`). An install via `go install` ships only the binary,
// not the repo's docs/ tree, so the guides must travel inside the binary.
package tips

import (
	"embed"

	"danny.vn/secops/internal/embeddocs"
)

//go:embed *.md
var allFiles embed.FS

//go:embed 12-parser-extensions.md
var parserExtensions string

// ParserExtensions returns the parser-extension authoring guide (Markdown).
func ParserExtensions() string { return parserExtensions }

// Entry is one embedded tip file: its base name, first heading, and content.
type Entry = embeddocs.Entry

// All returns every embedded *.md file as an Entry, sorted by filename.
func All() []Entry { return embeddocs.All(allFiles) }
