// Package mirror snapshots live SecOps state to local files (pull) and pushes
// local rule changes back (push). It is the file-mirroring layer on top of the
// pure danny.vn/secops/chronicle SDK; all on-disk layout, slugging, YAML
// formatting, and secret redaction live here, never in the SDK.
package mirror

import (
	"os"
	"regexp"
	"strings"
)

// On-disk subdirectory names, one per artifact type.
const (
	DirRules      = "rules"
	DirRefLists   = "reference_lists"
	DirDataTables = "data_tables"
	DirDashboards = "dashboards"
	DirCurated    = "curated"
	DirFeeds      = "feeds"
	DirParsers    = "parsers"
)

var slugRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Slugify makes s safe as a filename component: it keeps [A-Za-z0-9._-],
// collapses any other run of characters to a single "_", trims leading/trailing
// "_", and returns "_unnamed" when the result would otherwise be empty.
func Slugify(s string) string {
	out := strings.Trim(slugRE.ReplaceAllString(s, "_"), "_")
	if out == "" {
		return "_unnamed"
	}
	return out
}

// EnsureDir creates path (and parents) if needed and returns it.
func EnsureDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", err
	}
	return path, nil
}

// DataRoot returns base if non-empty, else the current working directory.
func DataRoot(base string) string {
	if base != "" {
		return base
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// lastSegment returns the final "/"-delimited component of a resource name.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
