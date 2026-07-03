// Package tips embeds the parser-extension authoring guide so the compiled
// binary can print it (`ingest parsers extension tips`). An install via
// `go install danny.vn/secops/cmd/secopsctl@latest` ships only the binary, not
// the repo's docs/ tree, so the guide has to travel inside the binary.
package tips

import _ "embed"

//go:embed 12-parser-extensions.md
var parserExtensions string

// ParserExtensions returns the parser-extension authoring guide (Markdown).
func ParserExtensions() string { return parserExtensions }
