package cli

// Action summarization, catalog helpers, and any-field utilities for the
// playbook components subcommands. See soar_playbook_components.go for the
// command constructors, row types, integration resolution, and printers.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"danny.vn/secops/soar"
)

// wrapActionsEnvelope wraps per-action full bodies into the {"actions":[…]}
// envelope summarizeIntegrationActions walks, preserving each action's parameter
// schema. Empty bodies are skipped.
func wrapActionsEnvelope(raws []json.RawMessage) json.RawMessage {
	items := make([]json.RawMessage, 0, len(raws))
	for _, r := range raws {
		if len(r) > 0 {
			items = append(items, r)
		}
	}
	b, err := json.Marshal(map[string]any{"actions": items})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func summarizeIntegrationActions(integration string, raw json.RawMessage) []playbookActionRow {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var rows []playbookActionRow
	collectActionRows(integration, "", root, seen, &rows)
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

func collectActionRows(integration, key string, v any, seen map[string]struct{}, rows *[]playbookActionRow) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if m, ok := item.(map[string]any); ok && actionArrayKey(key) {
				if row, ok := actionRowFromMap(integration, m, true); ok {
					addActionRow(row, seen, rows)
				}
			}
			collectActionRows(integration, key, item, seen, rows)
		}
	case map[string]any:
		if row, ok := actionRowFromMap(integration, x, false); ok {
			addActionRow(row, seen, rows)
		}
		for childKey, child := range x {
			collectActionRows(integration, childKey, child, seen, rows)
		}
	}
}

func actionArrayKey(key string) bool {
	switch strings.ToLower(key) {
	case "actions", "integrationactions", "supportedactions", "integrationsupportedactions":
		return true
	default:
		return false
	}
}

func actionRowFromMap(integration string, m map[string]any, fromActionArray bool) (playbookActionRow, bool) {
	// Prefer the friendly displayName; the modern action object's `name` is the full
	// resource path, while the legacy details shape carries only the friendly `name`.
	name := stringAnyField(m, "displayName")
	if name == "" {
		name = stringAnyField(m, "name")
	}
	if name == "" {
		return playbookActionRow{}, false
	}
	if !fromActionArray && !looksLikeActionDefinition(m) {
		return playbookActionRow{}, false
	}
	row := playbookActionRow{
		Integration:         integration,
		ID:                  scalarAnyString(m["id"]),
		Name:                name,
		Description:         stringAnyField(m, "description"),
		ParameterCount:      arrayAnyLen(m["parameters"]),
		MandatoryParameters: mandatoryActionParameters(m["parameters"]),
		Parameters:          actionParameters(m["parameters"]),
		ScriptResultName:    stringAnyField(m, "scriptResultName"),
		ActionType:          scalarAnyString(m["actionType"]),
	}
	// Enabled/async carry different keys across the two surfaces (modern action GET:
	// `enabled`/`isAsync`; legacy details: `isEnabled`/`isAsync`).
	if b, ok := boolAnyFieldFirst(m, "enabled", "isEnabled"); ok {
		row.Enabled = &b
	}
	if b, ok := boolAnyField(m, "hasJsonResult"); ok {
		row.HasJSONResult = &b
	}
	if b, ok := boolAnyFieldFirst(m, "isAsync", "async"); ok {
		row.Async = &b
	}
	return row, true
}

// boolAnyFieldFirst returns the first present boolean among keys.
func boolAnyFieldFirst(m map[string]any, keys ...string) (bool, bool) {
	for _, k := range keys {
		if b, ok := boolAnyField(m, k); ok {
			return b, true
		}
	}
	return false, false
}

func looksLikeActionDefinition(m map[string]any) bool {
	for _, key := range []string{
		"scriptResultName",
		"dynamicResultsMetadata",
		"hasJsonResult",
		"actionType",
		"isAsync",
		"actionWidgetTemplateIdentifier",
		"defaultResultValue",
		"timeoutSeconds",
	} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func addActionRow(row playbookActionRow, seen map[string]struct{}, rows *[]playbookActionRow) {
	key := row.Integration + "\x00" + row.ID + "\x00" + row.Name
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*rows = append(*rows, row)
}

func filterActionRows(rows []playbookActionRow, grep string) []playbookActionRow {
	if strings.TrimSpace(grep) == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if matchesAny(grep, row.Name, row.Description, row.ID, row.ActionType) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// actionParameters extracts each input parameter's full schema (name/type/
// mandatory/default/optionalValues/description) — the detail an author needs to
// fill a step in. Returns nil when there are no parameters.
func actionParameters(v any) []playbookActionParam {
	params, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []playbookActionParam
	for _, raw := range params {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Param name and mandatory flag differ across surfaces: the modern action GET
		// uses `displayName`/`mandatory`; the legacy details use `name`/`isMandatory`.
		name := stringAnyField(p, "name")
		if name == "" {
			name = stringAnyField(p, "displayName")
		}
		if name == "" {
			continue
		}
		param := playbookActionParam{
			Name:         name,
			Type:         scalarAnyString(p["type"]),
			DefaultValue: scalarAnyString(p["defaultValue"]),
			Description:  stringAnyField(p, "description"),
		}
		if mandatory, ok := boolAnyFieldFirst(p, "mandatory", "isMandatory"); ok {
			param.Mandatory = mandatory
		}
		for _, ov := range anySlice(p["optionalValues"]) {
			if s := scalarAnyString(ov); s != "" {
				param.OptionalValues = append(param.OptionalValues, s)
			}
		}
		out = append(out, param)
	}
	return out
}

// anySlice returns v as a []any, or nil when it is not an array.
func anySlice(v any) []any {
	arr, _ := v.([]any)
	return arr
}

func mandatoryActionParameters(v any) []string {
	params, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range params {
		param, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		mandatory, ok := boolAnyFieldFirst(param, "mandatory", "isMandatory")
		if !ok || !mandatory {
			continue
		}
		name := stringAnyField(param, "name")
		if name == "" {
			name = stringAnyField(param, "displayName")
		}
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func matchesAny(needle string, values ...string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func boolPtrString(b *bool) string {
	if b == nil {
		return "-"
	}
	if *b {
		return "true"
	}
	return "false"
}

func stringAnyField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func boolAnyField(m map[string]any, key string) (bool, bool) {
	b, ok := m[key].(bool)
	return b, ok
}

func arrayAnyLen(v any) int {
	arr, ok := v.([]any)
	if !ok {
		return 0
	}
	return len(arr)
}

func scalarAnyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return fmt.Sprintf("%g", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// actionCatalogRow is one entry of the cross-integration action catalog
// (`integrations/-/actions`): the summary columns plus the numeric id.
type actionCatalogRow struct {
	ID          string `json:"id"`
	Integration string `json:"integration"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	Async       bool   `json:"async"`
	Custom      bool   `json:"custom"`
}

func actionCatalogRows(defs []soar.ActionDef, grep string) []actionCatalogRow {
	rows := make([]actionCatalogRow, 0, len(defs))
	for i := range defs {
		d := &defs[i]
		row := actionCatalogRow{
			ID:          d.PathID(),
			Integration: d.Integration,
			Name:        d.DisplayName,
			Description: d.Description,
			Enabled:     d.Enabled,
			Async:       d.Async,
			Custom:      d.Custom,
		}
		if matchesAny(grep, row.ID, row.Integration, row.Name, row.Description) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if a, b := strings.ToLower(rows[i].Integration), strings.ToLower(rows[j].Integration); a != b {
			return a < b
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

func printActionCatalogRows(w io.Writer, rows []actionCatalogRow) {
	fmt.Fprintln(w, "ID\tINTEGRATION\tNAME\tENABLED\tASYNC\tCUSTOM")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%t\t%t\n", row.ID, row.Integration, row.Name, row.Enabled, row.Async, row.Custom)
	}
	fmt.Fprintf(w, "\n%d action(s) across all integrations\n", len(rows))
}
