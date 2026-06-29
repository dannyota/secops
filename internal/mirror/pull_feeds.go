package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
)

// PullFeeds snapshots every ingestion feed to outDir as <slug>.yaml, with
// credential-bearing scalars under settings redacted before anything touches
// disk. It returns the number of feeds written. Read-only against the instance.
func PullFeeds(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	out, err := EnsureDir(outDir)
	if err != nil {
		return 0, err
	}

	feeds, err := c.ListFeeds(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	stateCount := map[string]int{}
	for _, f := range feeds {
		display := f.DisplayName
		if display == "" {
			display = f.UID
		}
		if display == "" {
			display = "unnamed"
		}
		stateCount[f.State]++

		rec := feedRecord(f, display)
		if err := writeYAML(filepath.Join(out, Slugify(display)+".yaml"), rec); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("feeds:        wrote %d -> %s/ (state: %s)\n", written, out, formatStateCount(stateCount))
	return written, nil
}

// feedRecord builds the sorted, empty-dropped on-disk record for one feed.
//
// DEVIATION: Details is a single freeform map on the SDK Feed (the API nests
// feedSourceType/logType/assetNamespace/labels alongside source-specific
// settings); we split it here rather than typing it, so settings can be
// recursed and redacted generically.
func feedRecord(f chronicle.Feed, display string) map[string]any {
	details := f.Details

	// last segment of details.logType (".../logTypes/<X>" -> "<X>")
	logType := ""
	if lt, _ := details["logType"].(string); lt != "" {
		logType = lt[strings.LastIndex(lt, "/")+1:]
	}

	settings := map[string]any{}
	for k, v := range details {
		switch k {
		case "feedSourceType", "logType", "assetNamespace", "labels":
			// pulled into their own fields below
		default:
			settings[k] = v
		}
	}

	rec := map[string]any{
		"display_name":         display,
		"uid":                  f.UID,
		"name":                 f.Name,
		"state":                f.State,
		"source_type":          details["feedSourceType"],
		"log_type":             logType,
		"asset_namespace":      details["assetNamespace"],
		"labels":               details["labels"],
		"settings":             stripSecrets(settings),
		"last_initiation_time": f.LastFeedInitiationTime,
		"failure_msg":          f.FailureMsg,
		"failure_details":      failureDetails(f.FailureDetails),
	}

	for k, v := range rec {
		if isEmptyValue(v) {
			delete(rec, k)
		}
	}
	return rec
}

// failureDetails decodes the raw failureDetails object for YAML output, or nil
// when absent/unparseable.
func failureDetails(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// isEmptyValue reports whether v should be dropped from the record: nil, the
// empty string, an empty map, or an empty slice (matching the Python guard
// "v not in (None, {}, [], "" )").
func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	default:
		return false
	}
}

// formatStateCount renders the per-state tally as Python's repr of a
// dict(sorted(...)) of {state: count}, e.g. {EMPTY: 1, ACTIVE: 3} with the
// keys single-quoted, matching the legacy CLI output exactly.
func formatStateCount(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("'%s': %d", k, counts[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
