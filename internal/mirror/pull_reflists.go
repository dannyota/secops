package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
)

// refListMeta is the on-disk `<slug>.yaml` companion to each `<slug>.txt` of
// reference-list values. It is a typed, stable subset of the live resource so
// the snapshot round-trips deterministically for clean git diffs.
type refListMeta struct {
	DisplayName        string `yaml:"display_name"`
	Name               string `yaml:"name"`
	Description        string `yaml:"description,omitempty"`
	SyntaxType         string `yaml:"syntax_type,omitempty"`
	ScopeInfo          any    `yaml:"scope_info,omitempty"`
	RevisionCreateTime string `yaml:"revision_create_time,omitempty"`
	EntryCount         int    `yaml:"entry_count"`
}

// PullReferenceLists snapshots every reference list to outDir. For each list it
// writes <slug>.txt — one entry Value per line, with a trailing newline only
// when the list is non-empty — and <slug>.yaml with the typed metadata. The
// slug is derived from DisplayName, falling back to the last segment of Name.
// Returns the number of lists written.
func PullReferenceLists(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	lists, err := c.ListReferenceLists(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	for _, rl := range lists {
		display := rl.DisplayName
		if display == "" {
			display = lastSegment(rl.Name)
		}
		slug := Slugify(display)

		lines := make([]string, 0, len(rl.Entries))
		for _, e := range rl.Entries {
			lines = append(lines, e.Value)
		}
		body := strings.Join(lines, "\n")
		if len(lines) > 0 {
			body += "\n"
		}
		if err := os.WriteFile(filepath.Join(outDir, slug+".txt"), []byte(body), 0o644); err != nil {
			return written, err
		}

		meta := refListMeta{
			DisplayName:        display,
			Name:               rl.Name,
			Description:        rl.Description,
			SyntaxType:         rl.SyntaxType,
			ScopeInfo:          decodeScopeInfo(rl.ScopeInfo),
			RevisionCreateTime: rl.RevisionCreateTime,
			EntryCount:         len(lines),
		}
		if err := writeYAML(filepath.Join(outDir, slug+".yaml"), meta); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("reflists:     wrote %d -> %s/\n", written, outDir)
	return written, nil
}

// decodeScopeInfo turns the freeform scopeInfo JSON into a generic value so it
// renders as nested YAML rather than an opaque quoted JSON string. Returns nil
// (omitted by the YAML tag) when empty or unparseable.
func decodeScopeInfo(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
