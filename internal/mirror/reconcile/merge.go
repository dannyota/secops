package reconcile

import (
	"encoding/json"
	"maps"
)

// DeepMerge overlays the operator's local edits onto the full live body and
// returns the merged JSON. It is the safe Update body for surfaces whose write
// shape is a whole-body replace: start from the live object (so server-managed
// and unedited fields are preserved verbatim) and apply only the fields present
// in local, recursing into nested objects.
//
// skip is consulted for every scalar/leaf value in local: when it returns true
// the local value is dropped and the live value kept. The caller uses this to
// never write a redaction marker back over a real secret — the masked field
// stays whatever the server already has. path is the dotted key path (for
// surface-specific rules); it is "" at the root.
//
// Arrays are replaced wholesale (not merged element-by-element): merging arrays
// by index silently corrupts ordered config (playbook steps, mapping rules), so
// a local array always replaces the live array as-is. Surfaces with ordered
// arrays that must preserve live-only elements should not use DeepMerge.
func DeepMerge(live, local json.RawMessage, skip func(path string, localVal any) bool) (json.RawMessage, error) {
	var liveV, localV any
	if len(live) > 0 {
		if err := json.Unmarshal(live, &liveV); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(local, &localV); err != nil {
		return nil, err
	}
	merged := mergeValue(liveV, localV, "", skip)
	return json.Marshal(merged)
}

func mergeValue(live, local any, path string, skip func(string, any) bool) any {
	localMap, localIsMap := local.(map[string]any)
	liveMap, liveIsMap := live.(map[string]any)
	if !localIsMap {
		// Leaf or array: honor skip, else take local (arrays replace wholesale).
		if skip != nil && skip(path, local) {
			return live
		}
		return local
	}
	out := map[string]any{}
	if liveIsMap {
		maps.Copy(out, liveMap)
	}
	for k, lv := range localMap {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		var existing any
		if liveIsMap {
			existing = liveMap[k]
		}
		out[k] = mergeValue(existing, lv, childPath, skip)
	}
	return out
}

// ContainsValue reports whether any scalar leaf in raw equals want — used to
// refuse a Create whose body still carries a redaction marker (you cannot invent
// a secret from a masked snapshot).
func ContainsValue(raw json.RawMessage, want string) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return containsValue(v, want)
}

func containsValue(v any, want string) bool {
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			if containsValue(val, want) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if containsValue(val, want) {
				return true
			}
		}
	case string:
		return t == want
	}
	return false
}
