// Package guides embeds the user-facing guide Markdown so the compiled binary
// can serve them as MCP resources. An install via `go install` ships only the
// binary, not the repo's docs/ tree, so the guides must travel inside it.
package guides

import (
	"embed"

	"danny.vn/secops/internal/embeddocs"
)

//go:embed *.md
var allFiles embed.FS

// Entry is one embedded guide file: its base name, first heading, and content.
type Entry = embeddocs.Entry

// All returns every embedded *.md file as an Entry, sorted by filename.
func All() []Entry { return embeddocs.All(allFiles) }
