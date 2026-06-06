package reconcile

import (
	"bytes"
	"encoding/json"
	"strings"
)

// timeKeys are server-stamped time fields stripped at ANY depth before
// canonicalization — they are never meaningful config and would otherwise make
// every diff permanently dirty. The set mirrors what the live smoke harness
// already strips (soar/legacy/live_support_test.go) plus the SIEM equivalents.
var timeKeys = map[string]bool{
	"creationTimeUnixTimeInMs":         true,
	"modificationTimeUnixTimeInMs":     true,
	"lastModificationTimeUnixTimeInMs": true,
	"creationTime":                     true,
	"modificationTime":                 true,
	"lastModificationTime":             true,
	"createTime":                       true,
	"updateTime":                       true,
	"revisionCreateTime":               true,
}

// topIdentityKeys are stripped only at the ROOT object: they are the object's
// own server identity (carried separately in Object.ServerID/Etag), not config.
// They are NOT stripped at depth, where a nested "id"/"etag" may be a meaningful
// reference to another object that the operator legitimately edits.
//
// "_server" is the reserved on-disk identity block a JSON surface injects at the
// root (server id/etag); it is stripped here so it never affects the diff basis.
var topIdentityKeys = map[string]bool{
	"id":      true,
	"etag":    true,
	"_server": true,
}

// Canonicalize returns a deterministic, diff-stable form of raw: it decodes to a
// generic value, strips server-managed/volatile keys, and re-encodes with sorted
// keys (encoding/json sorts map keys) and a 2-space indent. extraStrip names
// additional surface-specific keys to drop at any depth (e.g. server-echoed
// counts). The result is the canonical diff basis stored in Object.Canonical and
// written to disk, so `git diff` shows only real config changes.
//
// Redaction is the CALLER's responsibility and must happen BEFORE Canonicalize
// on both the local and live sides, so a masked secret never differs from itself.
func Canonicalize(raw json.RawMessage, extraStrip ...string) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	extra := make(map[string]bool, len(extraStrip))
	for _, k := range extraStrip {
		extra[k] = true
	}
	v = strip(v, extra, true)
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// strip recursively removes volatile keys. root distinguishes the top object
// (where identity keys are dropped) from nested objects (where they are kept).
func strip(v any, extra map[string]bool, root bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if timeKeys[k] || strings.HasSuffix(k, "UnixTimeInMs") || extra[k] {
				continue
			}
			if root && topIdentityKeys[k] {
				continue
			}
			out[k] = strip(val, extra, false)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = strip(val, extra, false)
		}
		return out
	default:
		return v
	}
}
