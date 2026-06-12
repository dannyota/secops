package mirror

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// redactedMarker replaces any sensitive value before it is written to disk.
const redactedMarker = "***REDACTED***"

// RedactPatternsFile is the optional file at the data root holding value-redaction
// regex patterns (one per line; blank lines and `#` comments ignored). It masks
// secrets that arrive as plain inline strings — e.g. a webhook URL carrying a
// token in a playbook step parameter — which escape the key-name redaction above.
const RedactPatternsFile = ".secopsctl-redact"

// sensitiveKeys are scalar field names that may carry credentials on feeds and
// similar entities. Their values are redacted before anything touches disk so a
// pulled snapshot is safe to commit.
var sensitiveKeys = map[string]bool{
	"password":            true,
	"secret":              true,
	"apiKey":              true,
	"api_key":             true,
	"token":               true,
	"privateKey":          true,
	"private_key":         true,
	"clientSecret":        true,
	"client_secret":       true,
	"authorizationHeader": true,
	"secretAccessKey":     true,
	"access_key":          true,
	"accessKey":           true,
	"authToken":           true,
	"auth_token":          true,
	"sharedKey":           true,
	"shared_key":          true,
}

// ValueRedactor masks substrings of string values that match any of its regex
// patterns. It complements the key-name redaction above for secrets that arrive
// as plain inline strings (no credential-typed field signals them). Patterns come
// from the data-root .secopsctl-redact file and/or the `--redact` flag.
type ValueRedactor struct {
	patterns []*regexp.Regexp
}

// NewValueRedactor compiles the given patterns. Blank entries are skipped; an
// invalid pattern is an error. Returns nil (a no-op redactor) when no usable
// pattern is supplied, so callers can pass the result straight through.
func NewValueRedactor(patterns []string) (*ValueRedactor, error) {
	var compiled []*regexp.Regexp
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid redaction pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	if len(compiled) == 0 {
		return nil, nil
	}
	return &ValueRedactor{patterns: compiled}, nil
}

// LoadRedactPatternsFile reads the patterns from <root>/.secopsctl-redact. A
// missing file is not an error (returns no patterns).
func LoadRedactPatternsFile(root string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, RedactPatternsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return strings.Split(string(b), "\n"), nil
}

// RedactJSON applies the value redactor to a raw JSON document, returning the
// re-encoded body. A nil redactor returns raw unchanged. int64 ids/timestamps are
// preserved (UseNumber) so a large id is not corrupted by a float round-trip.
func (r *ValueRedactor) RedactJSON(raw json.RawMessage) (json.RawMessage, error) {
	if r == nil || len(raw) == 0 {
		return raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(r.apply(v))
}

// apply walks decoded JSON and masks every pattern match inside each string value.
func (r *ValueRedactor) apply(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = r.apply(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = r.apply(val)
		}
		return t
	case string:
		for _, re := range r.patterns {
			t = re.ReplaceAllString(t, redactedMarker)
		}
		return t
	default:
		return v
	}
}

// valueRedactor is the process-wide value redactor, set once by the CLI from the
// data-root .secopsctl-redact file (and the `--redact` flag) before any engine
// operation. It is read by the surfaces that write inline-secret-bearing bodies
// (playbooks). pull/drift/push all load the SAME file from the same data root, so
// redaction is applied identically on every side — a value masked on pull is also
// masked when drift/push canonicalize the live object, so it never produces a
// phantom diff (cf. the key-name redaction guarantee).
var valueRedactor *ValueRedactor

// SetValueRedactor installs the process-wide value redactor (nil disables it).
func SetValueRedactor(r *ValueRedactor) { valueRedactor = r }

// redact recursively replaces the value of any sensitive key with a marker.
func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKeys[k] {
				out[k] = redactedMarker
			} else {
				out[k] = redact(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}
