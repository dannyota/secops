package legacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
)

func applyActionStepMold(body map[string]any, repl PlaybookStepReplacement) error {
	match := strings.TrimSpace(repl.Match)
	if match == "" {
		return fmt.Errorf("legacy: step replacement has empty match")
	}
	mold, err := decodePlaybookObject(repl.Mold, "step mold")
	if err != nil {
		return err
	}
	if err := validateActionStepMold(mold); err != nil {
		return err
	}
	steps, ok := body["steps"].([]any)
	if !ok {
		return fmt.Errorf("legacy: base playbook steps is %T, want array", body["steps"])
	}
	idx, err := findPlaybookStep(steps, match)
	if err != nil {
		return err
	}
	baseStep, ok := steps[idx].(map[string]any)
	if !ok {
		return fmt.Errorf("legacy: base playbook steps[%d] is %T, want object", idx, steps[idx])
	}
	steps[idx] = mergeActionStepMold(baseStep, mold)
	body["steps"] = steps
	return nil
}

func validateActionStepMold(mold map[string]any) error {
	if !isActionStepType(mold["type"]) {
		return fmt.Errorf("legacy: step mold is type %v, want action step (0 or \"ACTION\")", mold["type"])
	}
	if stringMapField(mold, "integration") == "" {
		return fmt.Errorf("legacy: step mold missing integration")
	}
	if stringMapField(mold, "actionName") == "" {
		return fmt.Errorf("legacy: step mold missing actionName")
	}
	if _, ok := mold["parameters"].([]any); !ok {
		return fmt.Errorf("legacy: step mold parameters is %T, want array", mold["parameters"])
	}
	return nil
}

func findPlaybookStep(steps []any, match string) (int, error) {
	found := -1
	for i, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("legacy: base playbook steps[%d] is %T, want object", i, raw)
		}
		if stringMapField(step, "name") == match ||
			stringMapField(step, "identifier") == match ||
			stringMapField(step, "originalStepIdentifier") == match {
			if found >= 0 {
				return 0, fmt.Errorf("legacy: multiple base playbook steps match %q", match)
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("legacy: no base playbook step matches %q", match)
	}
	return found, nil
}

var playbookStepMoldPreserveKeys = map[string]struct{}{
	"identifier":                   {},
	"originalStepIdentifier":       {},
	"workflowIdentifier":           {},
	"parentStepContainerId":        {},
	"startLoopStepIdentifier":      {},
	"parentWorkflowLoopIteration":  {},
	"loopIteration":                {},
	"loopName":                     {},
	"blockStepId":                  {},
	"id":                           {},
	"creationTimeUnixTimeInMs":     {},
	"modificationTimeUnixTimeInMs": {},
	"additionalProperties":         {},
}

func mergeActionStepMold(baseStep, mold map[string]any) map[string]any {
	out := make(map[string]any, len(baseStep)+len(mold))
	maps.Copy(out, baseStep)
	for k, v := range mold {
		if _, preserve := playbookStepMoldPreserveKeys[k]; preserve {
			continue
		}
		out[k] = v
	}
	return out
}

func decodePlaybookObject(raw json.RawMessage, label string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("legacy: parse %s: %w", label, err)
	}
	if out == nil {
		return nil, fmt.Errorf("legacy: %s is null, want object", label)
	}
	return out, nil
}

func decodeJSONValue(raw json.RawMessage, label string) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("legacy: parse %s: %w", label, err)
	}
	if out == nil {
		return nil, fmt.Errorf("legacy: %s is null, want JSON object/array/scalar", label)
	}
	return out, nil
}

func stringMapField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// isActionStepType returns true when v represents an action step type —
// either the integer 0 or the string "ACTION".
func isActionStepType(v any) bool {
	if s, ok := v.(string); ok {
		return s == "ACTION"
	}
	code, ok := intFieldValue(v)
	return ok && code == 0
}

func intFieldValue(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case float64:
		i := int64(n)
		return i, float64(i) == n
	case int:
		return int64(n), true
	case int64:
		return n, true
	case string:
		parsed := json.Number(n)
		i, err := parsed.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

// splicePlaybookRelations rewires the anchor's selected outgoing relation
// through newID (anchor->new keeps the branch condition; new->successor gets an
// empty condition), or appends anchor->new when the anchor is a tail step.
func splicePlaybookRelations(body map[string]any, anchorID, newID, branch string) ([]any, error) {
	var relations []any
	switch v := body["stepsRelations"].(type) {
	case nil:
	case []any:
		relations = v
	default:
		return nil, fmt.Errorf("legacy: playbook stepsRelations is %T, want array", v)
	}
	branch = strings.TrimSpace(branch)

	var outgoing []map[string]any
	var conditions []string
	for _, raw := range relations {
		rel, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("legacy: stepsRelations entry is %T, want object", raw)
		}
		if stringMapField(rel, "fromStep") != anchorID {
			continue
		}
		cond := stringMapField(rel, "condition")
		conditions = append(conditions, strconv.Quote(cond))
		if branch == "" || cond == branch {
			outgoing = append(outgoing, rel)
		}
	}

	newRelation := func(from, to, condition string) map[string]any {
		return map[string]any{"fromStep": from, "toStep": to, "condition": condition}
	}
	switch {
	case len(outgoing) == 0 && branch == "":
		// Tail anchor — append after it.
		return append(relations, newRelation(anchorID, newID, "")), nil
	case len(outgoing) == 0:
		return nil, fmt.Errorf("legacy: anchor has no outgoing relation with condition %q (it has: %s)",
			branch, strings.Join(conditions, ", "))
	case len(outgoing) > 1:
		return nil, fmt.Errorf("legacy: anchor has %d outgoing relations (%s) — select one with Branch",
			len(outgoing), strings.Join(conditions, ", "))
	}
	successor := stringMapField(outgoing[0], "toStep")
	outgoing[0]["toStep"] = newID
	return append(relations, newRelation(newID, successor, "")), nil
}

// uniqueStepInstanceName mints the designer-style "<actionName>_<n>" instance
// name, picking the first n free among the existing steps.
func uniqueStepInstanceName(steps []any, actionName string) string {
	if actionName == "" {
		actionName = "Step"
	}
	used := make(map[string]struct{}, len(steps))
	for _, raw := range steps {
		if step, ok := raw.(map[string]any); ok {
			used[stringMapField(step, "instanceName")] = struct{}{}
		}
	}
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s_%d", actionName, n)
		if _, taken := used[candidate]; !taken {
			return candidate
		}
	}
}
