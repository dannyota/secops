package legacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// PlaybookBuildOptions controls an offline playbook composition pass. The base
// playbook and every step mold should come from SOAR-exported JSON; this helper
// edits that known-good shape rather than inventing playbook bodies.
type PlaybookBuildOptions struct {
	Name             string
	CronSchedule     string
	StepReplacements []PlaybookStepReplacement
}

// PlaybookStepReplacement replaces one step in the base playbook with an
// exported integration-action step mold while preserving the base step's graph
// identity fields. Match resolves against the base step name, identifier, or
// originalStepIdentifier.
type PlaybookStepReplacement struct {
	Match string
	Mold  json.RawMessage
}

// BuildPlaybookFromMolds composes a save-ready playbook body from a full base
// playbook and exported step molds. It is offline only: callers should still run
// the normal playbook dry-run/save path for SOAR validation.
func BuildPlaybookFromMolds(base Playbook, opts PlaybookBuildOptions) (Playbook, error) {
	body, err := decodePlaybookObject(base, "base playbook")
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(opts.Name); name != "" {
		body["name"] = name
	}
	if cron := strings.TrimSpace(opts.CronSchedule); cron != "" {
		if err := setPlaybookCronSchedule(body, cron); err != nil {
			return nil, err
		}
	}
	for _, repl := range opts.StepReplacements {
		if err := applyActionStepMold(body, repl); err != nil {
			return nil, err
		}
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("legacy: marshal built playbook: %w", err)
	}
	return preparePlaybookForSave(out)
}

// ExtractActionStepMold returns one exported action step from a playbook. The
// result is suitable for BuildPlaybookFromMolds; that later merge preserves the
// destination playbook's graph identity fields.
func ExtractActionStepMold(playbook Playbook, match string) (json.RawMessage, error) {
	body, err := decodePlaybookObject(playbook, "playbook")
	if err != nil {
		return nil, err
	}
	steps, ok := body["steps"].([]any)
	if !ok {
		return nil, fmt.Errorf("legacy: playbook steps is %T, want array", body["steps"])
	}
	idx, err := findPlaybookStep(steps, strings.TrimSpace(match))
	if err != nil {
		return nil, err
	}
	step, ok := steps[idx].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("legacy: playbook steps[%d] is %T, want object", idx, steps[idx])
	}
	if err := validateActionStepMold(step); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(step)
	if err != nil {
		return nil, fmt.Errorf("legacy: marshal step mold: %w", err)
	}
	return raw, nil
}

func setPlaybookCronSchedule(body map[string]any, cron string) error {
	trigger, ok := body["trigger"].(map[string]any)
	if !ok || trigger == nil {
		return fmt.Errorf("legacy: base playbook has no trigger object; build from an exported scheduled playbook")
	}
	trigger["cronSchedule"] = cron
	return nil
}

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
	stepType, ok := intFieldValue(mold["type"])
	if !ok || stepType != 0 {
		return fmt.Errorf("legacy: step mold is type %v, want action step type 0", mold["type"])
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

func stringMapField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
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
