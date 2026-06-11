package legacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
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

// PlaybookTriggerPatchOptions controls an offline trigger-edit pass. Empty fields
// are preserved; pointer fields distinguish "not provided" from false/empty.
type PlaybookTriggerPatchOptions struct {
	PlaybookEnabled    *bool
	TriggerEnabled     *bool
	Type               any
	ExecutionMode      string
	CronSchedule       *string
	Conditions         json.RawMessage
	ReactionConditions json.RawMessage
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

// PatchPlaybookTrigger edits the trigger fields that secopsctl can safely
// represent, preserving every unknown playbook and trigger field. It is offline
// only; callers should validate/save the returned body through the normal guarded
// playbook path.
func PatchPlaybookTrigger(playbook Playbook, opts PlaybookTriggerPatchOptions) (Playbook, error) {
	body, err := decodePlaybookObject(playbook, "playbook")
	if err != nil {
		return nil, err
	}
	if opts.PlaybookEnabled != nil {
		body["isEnabled"] = *opts.PlaybookEnabled
	}
	trigger, err := mutablePlaybookTrigger(body)
	if err != nil {
		return nil, err
	}
	if opts.TriggerEnabled != nil {
		trigger["isEnabled"] = *opts.TriggerEnabled
	}
	if opts.Type != nil {
		trigger["type"] = opts.Type
	}
	if strings.TrimSpace(opts.ExecutionMode) != "" {
		trigger["executionMode"] = strings.TrimSpace(opts.ExecutionMode)
	}
	if opts.CronSchedule != nil {
		trigger["cronSchedule"] = *opts.CronSchedule
	}
	if len(bytes.TrimSpace(opts.Conditions)) > 0 {
		v, err := decodeJSONValue(opts.Conditions, "conditions")
		if err != nil {
			return nil, err
		}
		trigger["conditions"] = v
	}
	if len(bytes.TrimSpace(opts.ReactionConditions)) > 0 {
		v, err := decodeJSONValue(opts.ReactionConditions, "reaction conditions")
		if err != nil {
			return nil, err
		}
		trigger["reactionConditions"] = v
	}
	body["trigger"] = trigger
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("legacy: marshal trigger-patched playbook: %w", err)
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

func mutablePlaybookTrigger(body map[string]any) (map[string]any, error) {
	raw, ok := body["trigger"]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	trigger, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("legacy: playbook trigger is %T, want object", raw)
	}
	return trigger, nil
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

// PlaybookStepInsertOptions describes a brand-new action step spliced into a
// playbook after an existing anchor step. The mold supplies the wired action
// body (an ExtractActionStepMold result, or a step exported from the
// designer); the insert mints FRESH graph identity for it and rewires the
// anchor's outgoing relation through the new step.
type PlaybookStepInsertOptions struct {
	Mold json.RawMessage
	// After matches the anchor step by name, identifier, or
	// originalStepIdentifier.
	After string
	// Branch selects which outgoing relation of the anchor to splice when the
	// anchor has several (the relation's condition value, e.g. "1" for a
	// condition step's first branch). Empty requires the anchor to have at
	// most one outgoing relation; a tail anchor (none) appends the new step
	// after it.
	Branch string
	// InstanceName overrides the generated unique instance name
	// ("<actionName>_<n>", the designer's convention).
	InstanceName string
	// NewIdentifier pins the minted step identifier (tests); empty mints a
	// random UUID.
	NewIdentifier string
}

// playbookStepInsertDropKeys are mold fields that must NOT follow a step into
// a new graph position: container/loop placement and debug residue belong to
// the mold's source location.
var playbookStepInsertDropKeys = []string{
	"parentStepContainerId", "startLoopStepIdentifier",
	"parentWorkflowLoopIteration", "loopIteration", "loopName",
	"blockStepId", "debugData",
}

// InsertActionStep splices a brand-new action step into a playbook after the
// anchor step selected by opts.After, preserving every unknown field of both
// the playbook and the mold. Offline only — the result still flows through
// the normal validate → guarded-save path, where SOAR is the final validator.
func InsertActionStep(playbook Playbook, opts PlaybookStepInsertOptions) (Playbook, error) {
	body, err := decodePlaybookObject(playbook, "playbook")
	if err != nil {
		return nil, err
	}
	steps, ok := body["steps"].([]any)
	if !ok {
		return nil, fmt.Errorf("legacy: playbook steps is %T, want array", body["steps"])
	}
	anchorIdx, err := findPlaybookStep(steps, strings.TrimSpace(opts.After))
	if err != nil {
		return nil, err
	}
	anchor, _ := steps[anchorIdx].(map[string]any)
	anchorID := stringMapField(anchor, "identifier")
	if anchorID == "" {
		return nil, fmt.Errorf("legacy: anchor step %q has no identifier", opts.After)
	}

	mold, err := decodePlaybookObject(opts.Mold, "step mold")
	if err != nil {
		return nil, err
	}
	if err := validateActionStepMold(mold); err != nil {
		return nil, err
	}

	newID := strings.TrimSpace(opts.NewIdentifier)
	if newID == "" {
		if newID, err = randomUUID(); err != nil {
			return nil, err
		}
	}
	step := make(map[string]any, len(mold)+4)
	maps.Copy(step, mold)
	for _, k := range playbookStepInsertDropKeys {
		delete(step, k)
	}
	step["identifier"] = newID
	step["originalStepIdentifier"] = newID
	// A fresh step carries no source-scoped metadata.
	step["additionalProperties"] = map[string]any{}
	step["id"] = "0"
	step["creationTimeUnixTimeInMs"] = "0"
	step["modificationTimeUnixTimeInMs"] = "0"
	if wf := stringMapField(body, "identifier"); wf != "" {
		step["workflowIdentifier"] = wf
	}
	instance := strings.TrimSpace(opts.InstanceName)
	if instance == "" {
		instance = uniqueStepInstanceName(steps, stringMapField(step, "actionName"))
	}
	step["instanceName"] = instance

	relations, err := splicePlaybookRelations(body, anchorID, newID, opts.Branch)
	if err != nil {
		return nil, err
	}
	body["stepsRelations"] = relations
	body["steps"] = append(steps, step)

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("legacy: marshal playbook with inserted step: %w", err)
	}
	return preparePlaybookForSave(out)
}

// splicePlaybookRelations rewires the anchor's selected outgoing relation
// through newID (anchor→new keeps the branch condition; new→successor gets an
// empty condition), or appends anchor→new when the anchor is a tail step.
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
