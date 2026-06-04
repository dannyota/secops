package mirror

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
)

// PullParsers snapshots the single ACTIVE parser per log type to outDir as
// <logType>.conf (base64-decoded CBN source) plus <logType>.yaml (metadata). It
// returns the number of log types written. Read-only against the instance.
//
// When logTypes is nil, the set is derived from configured feeds: the last
// segment of each feed's Details["logType"], sorted and de-duplicated. Per-type
// errors (list failure, no ACTIVE parser, bad base64) are warned to stderr and
// skipped so one bad log type never aborts the pull.
func PullParsers(ctx context.Context, c *chronicle.Client, outDir string, logTypes []string) (int, error) {
	out, err := EnsureDir(outDir)
	if err != nil {
		return 0, err
	}

	if logTypes == nil {
		logTypes, err = logTypesInUse(ctx, c)
		if err != nil {
			return 0, err
		}
	}
	if len(logTypes) == 0 {
		fmt.Println("parsers:      no log types in use, nothing to pull")
		return 0, nil
	}

	written := 0
	for _, lt := range logTypes {
		parsers, err := c.ListParsers(ctx, lt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  (warn) list parsers for %s: %v\n", lt, err)
			continue
		}

		var active *chronicle.Parser
		inactive := 0
		for i := range parsers {
			if parsers[i].State == "ACTIVE" && active == nil {
				active = &parsers[i]
			} else if parsers[i].State != "ACTIVE" {
				inactive++
			}
		}
		if active == nil {
			fmt.Fprintf(os.Stderr, "  (warn) no ACTIVE parser for %s\n", lt)
			continue
		}

		cbnText := ""
		if active.CBN != "" {
			decoded, derr := base64.StdEncoding.DecodeString(active.CBN)
			if derr != nil {
				fmt.Fprintf(os.Stderr, "  (warn) decode cbn for %s: %v\n", lt, derr)
			} else {
				cbnText = string(decoded)
			}
		}

		if err := os.WriteFile(filepath.Join(out, lt+".conf"), []byte(cbnText), 0o644); err != nil {
			return written, err
		}

		meta := map[string]any{
			"log_type":              lt,
			"parser_id":             active.Name[strings.LastIndex(active.Name, "/")+1:],
			"name":                  active.Name,
			"creator_source":        mapString(active.Creator, "source"),
			"state":                 active.State,
			"type":                  active.Type,
			"release_stage":         active.ReleaseStage,
			"create_time":           active.CreateTime,
			"version":               active.VersionInfo["version"],
			"rollback_available":    active.VersionInfo["rollbackAvailable"],
			"inactive_parser_count": inactive,
			"cbn_bytes":             len(cbnText),
		}
		if err := writeYAML(filepath.Join(out, lt+".yaml"), meta); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("parsers:      wrote %d log types -> %s/ (active parser only)\n", written, out)
	return written, nil
}

// logTypesInUse returns the sorted, unique set of log types referenced by
// configured feeds (last segment of each feed's Details["logType"]).
func logTypesInUse(ctx context.Context, c *chronicle.Client) ([]string, error) {
	feeds, err := c.ListFeeds(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range feeds {
		lt, _ := f.Details["logType"].(string)
		if lt == "" {
			continue
		}
		seen[lt[strings.LastIndex(lt, "/")+1:]] = true
	}
	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	return types, nil
}

// mapString returns m[key] as a string, or "" when absent or non-string. Used
// to pluck scalars out of the freeform Creator/VersionInfo metadata maps.
func mapString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
