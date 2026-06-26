package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Generic raw-JSON readers and scalar formatters shared by the SOAR playbook
// output commands (split from soar_playbook_output.go to keep that file within the
// file-length budget). No command wiring here — pure helpers over json.RawMessage.

func rawRecordList(raw json.RawMessage) []json.RawMessage {
	if records, err := rawListRecords(raw); err == nil && len(records) > 0 {
		return records
	}
	root, ok := rawJSONObject(raw)
	if !ok {
		return nil
	}
	for _, key := range []string{"items", "data", "payload", "results", "actionResults", "logs", "objects"} {
		if records := rawArray(root[key]); len(records) > 0 {
			return records
		}
		if nested, ok := rawJSONObject(root[key]); ok {
			if records, err := rawListRecords(root[key]); err == nil && len(records) > 0 {
				return records
			}
			for _, nestedKey := range []string{"items", "records", "objectsList", "results", "logs"} {
				if records := rawArray(nested[nestedKey]); len(records) > 0 {
					return records
				}
			}
		}
	}
	return nil
}

func rawJSONObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	for _, key := range []string{"payload", "data", "result", "response"} {
		if nested, ok := rawJSONObject(m[key]); ok {
			return nested, true
		}
	}
	return m, true
}

func rawArray(raw json.RawMessage) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil
	}
	return records
}

func jsonArrayLen(raw json.RawMessage) int {
	return len(rawArray(raw))
}

func hasJSONValue(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func printJSONField(w io.Writer, m map[string]json.RawMessage, key, label string) {
	if !hasJSONValue(m, key) {
		return
	}
	if value := rawScalarString(m[key]); value != "" {
		fmt.Fprintf(w, "%s: %s\n", label, value)
	}
}

func rawScalarString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return ""
	}
	return displayJSONScalar(v)
}

func firstJSONByte(raw json.RawMessage) byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}
	return raw[0]
}

func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func displayJSONScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case json.Number:
		return x.String()
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func numericJSONValue(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case float64:
		n := int64(x)
		return n, float64(n) == x
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func stepLabel(step playbookStepDoc, idx int) string {
	if step.Name != "" {
		return step.Name
	}
	if step.Identifier != "" {
		return step.Identifier
	}
	return fmt.Sprintf("#%d", idx)
}

func defaultString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
