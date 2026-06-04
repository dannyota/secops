package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/secops/chronicle"
)

// PullDashboards snapshots every native dashboard to outDir as <slug>.json,
// where the slug is derived from DisplayName (falling back to the last segment
// of Name).
//
// CUSTOM dashboards are fully exported (charts + queries) via ExportDashboard so
// they round-trip on re-import; CURATED dashboards have no export form, so their
// listing entry (NativeDashboard.Raw) is written as-is. If an export fails the
// listing entry is used as a fallback and a warning is printed to stderr.
// Returns the number of dashboards written.
//
// DEVIATION: the JSON is pretty-printed with json.Indent (preserving the
// server's key order) rather than re-marshalled with sorted keys as the legacy
// Python tool did; this keeps the export bytes faithful to the API response.
func PullDashboards(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	dashboards, err := c.ListNativeDashboards(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	customCount := 0
	for _, d := range dashboards {
		display := d.DisplayName
		if display == "" {
			display = lastSegment(d.Name)
		}
		slug := Slugify(display)

		payload := d.Raw
		if d.Type == "CUSTOM" {
			if exported, err := c.ExportDashboard(ctx, d.Name); err != nil {
				fmt.Fprintf(os.Stderr, "  (warn) ExportDashboard(%s): %v\n", display, err)
			} else {
				payload = exported
				customCount++
			}
		}

		pretty, err := indentJSON(payload)
		if err != nil {
			return written, fmt.Errorf("dashboard %s: %w", display, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, slug+".json"), pretty, 0o644); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("dashboards:   wrote %d -> %s/  (full export for %d CUSTOM)\n", written, outDir, customCount)
	return written, nil
}

// indentJSON pretty-prints raw with a 2-space indent and a trailing newline.
func indentJSON(raw json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
