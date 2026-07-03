package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type soarPlaybookListRow struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Category   string `json:"category,omitempty"`
	Type       string `json:"type,omitempty"`
	ID         string `json:"id,omitempty"`
	Identifier string `json:"identifier,omitempty"`
}

type soarPlaybookValidationResult struct {
	File          string   `json:"file"`
	Name          string   `json:"name,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	Category      string   `json:"category,omitempty"`
	TriggerType   string   `json:"trigger_type,omitempty"`
	ExecutionMode string   `json:"execution_mode,omitempty"`
	Steps         int      `json:"steps"`
	ActionSteps   int      `json:"action_steps"`
	BlockSteps    int      `json:"block_steps"`
	Automatic     int      `json:"automatic_steps"`
	Manual        int      `json:"manual_steps"`
	Relations     int      `json:"relations"`
	Warnings      []string `json:"warnings,omitempty"`
}

type playbookDoc struct {
	Name          string            `json:"name"`
	IsEnabled     *bool             `json:"isEnabled"`
	CategoryName  string            `json:"categoryName"`
	Trigger       json.RawMessage   `json:"trigger"`
	Steps         []json.RawMessage `json:"steps"`
	StepsRelation []json.RawMessage `json:"stepsRelations"`
}

type playbookTriggerDoc struct {
	Type          any `json:"type"`
	ExecutionMode any `json:"executionMode"`
}

type playbookStepDoc struct {
	Identifier             string `json:"identifier"`
	OriginalStepIdentifier string `json:"originalStepIdentifier"`
	Name                   string `json:"name"`
	Type                   any    `json:"type"`
	IsAutomatic            *bool  `json:"isAutomatic"`
	Integration            string `json:"integration"`
	ActionName             string `json:"actionName"`
}

type playbookRelationDoc struct {
	FromStep string `json:"fromStep"`
	ToStep   string `json:"toStep"`
}

func newSOARPlaybookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playbooks",
		Short: "Discover and validate SOAR playbooks",
		Long: "Discover live SOAR playbooks and validate exported playbook JSON before\n" +
			"a guarded save. This is a SecOps interaction surface: local files are\n" +
			"only review/preflight artifacts before SecOps validates and runs them.",
	}
	cmd.AddCommand(
		newSOARPlaybookGetCmd(),
		newSOARPlaybookListCmd(),
		newSOARPlaybookLintCmd(),
		newSOARPlaybookHealthCmd(),
		newSOARPlaybookDiffCmd(),
		newSOARPlaybookDuplicateCmd(),
		newSOARPlaybookDeleteCmd(),
		newSOARPlaybookDeployCmd(),
		newSOARPlaybookMoveCmd(),
		newSOARPlaybookCategoriesCmd(),
		newSOARPlaybookValidateCmd(),
		newSOARPlaybookComponentsCmd(),
		newSOARPlaybookMoldCmd(),
		newSOARPlaybookTriggerCmd(),
		newSOARPlaybookTestCasesCmd(),
		newSOARPlaybookRunCmd(),
		newSOARPlaybookDebugCmd(),
		newSOARPlaybookDebugStepDataCmd(),
		newSOARPlaybookSimulationEnrichmentCmd(),
		newSOARPlaybookPendingCmd(),
		newSOARPlaybookStepCmd(),
		newSOARPlaybookRerunCmd(),
		newSOARPlaybookRerunBlockCmd(),
		newSOARPlaybookSummaryCmd(),
		newSOARPlaybookResultsCmd(),
		newSOARPlaybookResultCmd(),
		newSOARPlaybookPythonLogsCmd(),
		newSOARPlaybookVersionsCmd(),
		newSOARPlaybookRestoreCmd(),
		newSOARPlaybookStatsCmd(),
		newSOARPlaybookExportCmd(),
		newSOARPlaybookImportCmd(),
		func() *cobra.Command { c := newSOARPlaybookGenerateCmd(); c.Hidden = true; return c }(),
		newSOARPlaybookGenerateStatusCmd(),
	)
	return cmd
}

func newSOARPlaybookListCmd() *cobra.Command {
	var (
		enabledOnly bool
		types       []string
		search      string
		category    string
		sortBy      string
		desc        bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List live SOAR playbooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			cards, err := lc.ListPlaybooks(baseContext(), normalizePlaybookTypes(types))
			if err != nil {
				return err
			}
			rows := make([]soarPlaybookListRow, 0, len(cards))
			searchLower := strings.ToLower(search)
			categoryLower := strings.ToLower(category)
			for _, card := range cards {
				if enabledOnly && !card.IsEnabled {
					continue
				}
				if search != "" && !strings.Contains(strings.ToLower(card.Name), searchLower) {
					continue
				}
				if category != "" && !strings.EqualFold(card.CategoryName, categoryLower) {
					continue
				}
				row := soarPlaybookListRow{
					Name:       card.Name,
					Enabled:    card.IsEnabled,
					Category:   card.CategoryName,
					ID:         card.ID.String(),
					Identifier: card.Identifier,
				}
				enrichPlaybookRow(&row, card.Raw)
				rows = append(rows, row)
			}
			sortPlaybookRows(rows, sortBy, desc)
			if jsonOut {
				return emitJSON(rows)
			}
			printSOARPlaybookRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&enabledOnly, "enabled-only", false, "show only enabled playbooks")
	f.StringArrayVar(&types, "type", nil, "playbook type filter: regular or nested (repeatable)")
	f.StringVar(&search, "search", "", "case-insensitive name substring filter")
	f.StringVar(&category, "category", "", "exact category name filter (case-insensitive)")
	f.StringVar(&sortBy, "sort", "name", "sort by: name, type, category, enabled")
	f.BoolVar(&desc, "desc", false, "sort descending")
	return markJSON(cmd)
}

func enrichPlaybookRow(row *soarPlaybookListRow, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var extra struct {
		PlaybookType any `json:"playbookType"`
	}
	if json.Unmarshal(raw, &extra) == nil {
		row.Type = displayJSONScalar(extra.PlaybookType)
	}
}

func sortPlaybookRows(rows []soarPlaybookListRow, by string, desc bool) {
	less := func(i, j int) bool {
		switch strings.ToLower(by) {
		case "type":
			return strings.ToLower(rows[i].Type) < strings.ToLower(rows[j].Type)
		case "category":
			return strings.ToLower(rows[i].Category) < strings.ToLower(rows[j].Category)
		case "enabled":
			return !rows[i].Enabled && rows[j].Enabled
		default:
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		}
	}
	if desc {
		sort.Slice(rows, func(i, j int) bool { return less(j, i) })
	} else {
		sort.Slice(rows, func(i, j int) bool { return less(i, j) })
	}
}

func newSOARPlaybookValidateCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "validate --file <playbook.json>",
		Short: "Validate an exported playbook JSON file before save",
		Long: "Validate an exported SOAR playbook JSON file before a guarded save.\n" +
			"The command performs the same local save-shape checks as `soar push\n" +
			"playbook --dry-run` and reports trigger/step/action shape so the next\n" +
			"SecOps save or debug run is easier to review.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			if err := legacy.ValidatePlaybookForSave(json.RawMessage(raw)); err != nil {
				return err
			}
			res, err := summarizePlaybookJSON(file, raw)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			printSOARPlaybookValidation(cmd.OutOrStdout(), res)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "playbook JSON file to validate (required)")
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}

func normalizePlaybookTypes(types []string) []string {
	out := make([]string, 0, len(types))
	for _, typ := range types {
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "":
		case "regular":
			out = append(out, "REGULAR")
		case "nested", "block":
			out = append(out, "NESTED")
		default:
			out = append(out, typ)
		}
	}
	return out
}

func printSOARPlaybookRows(w io.Writer, rows []soarPlaybookListRow) {
	fmt.Fprintln(w, "ENABLED\tNAME\tTYPE\tCATEGORY\tIDENTIFIER")
	for _, row := range rows {
		fmt.Fprintf(w, "%t\t%s\t%s\t%s\t%s\n", row.Enabled, row.Name, row.Type, row.Category, row.Identifier)
	}
}

func summarizePlaybookJSON(file string, raw []byte) (soarPlaybookValidationResult, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc playbookDoc
	if err := dec.Decode(&doc); err != nil {
		return soarPlaybookValidationResult{}, fmt.Errorf("parse playbook JSON: %w", err)
	}

	res := soarPlaybookValidationResult{
		File:      file,
		Name:      doc.Name,
		Enabled:   doc.IsEnabled,
		Category:  doc.CategoryName,
		Steps:     len(doc.Steps),
		Relations: len(doc.StepsRelation),
	}
	if len(doc.Trigger) > 0 && string(doc.Trigger) != "null" {
		var trigger playbookTriggerDoc
		if err := json.Unmarshal(doc.Trigger, &trigger); err == nil {
			res.TriggerType = displayJSONScalar(trigger.Type)
			res.ExecutionMode = displayJSONScalar(trigger.ExecutionMode)
		}
	}

	stepIDs := map[string]bool{}
	for i, rawStep := range doc.Steps {
		var step playbookStepDoc
		if err := json.Unmarshal(rawStep, &step); err != nil {
			return soarPlaybookValidationResult{}, fmt.Errorf("decode step %d: %w", i, err)
		}
		if step.Identifier == "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("step %d has no identifier", i))
		} else {
			stepIDs[step.Identifier] = true
		}
		if step.OriginalStepIdentifier != "" {
			stepIDs[step.OriginalStepIdentifier] = true
		}
		switch n, ok := numericJSONValue(step.Type); {
		case ok && n == 0:
			res.ActionSteps++
			if step.Integration == "" || step.ActionName == "" {
				res.Warnings = append(res.Warnings, fmt.Sprintf("action step %q missing integration/actionName", stepLabel(step, i)))
			}
		case ok && n == 5:
			res.BlockSteps++
		}
		if step.IsAutomatic != nil && *step.IsAutomatic {
			res.Automatic++
		} else {
			res.Manual++
		}
	}
	for i, rawRelation := range doc.StepsRelation {
		var rel playbookRelationDoc
		if err := json.Unmarshal(rawRelation, &rel); err != nil {
			return soarPlaybookValidationResult{}, fmt.Errorf("decode relation %d: %w", i, err)
		}
		if rel.FromStep != "" && !stepIDs[rel.FromStep] {
			res.Warnings = append(res.Warnings, fmt.Sprintf("relation %d references unknown fromStep %q", i, rel.FromStep))
		}
		if rel.ToStep != "" && !stepIDs[rel.ToStep] {
			res.Warnings = append(res.Warnings, fmt.Sprintf("relation %d references unknown toStep %q", i, rel.ToStep))
		}
	}
	return res, nil
}

func printSOARPlaybookValidation(w io.Writer, res soarPlaybookValidationResult) {
	fmt.Fprintf(w, "playbook: %s\n", defaultString(res.Name, "(unnamed)"))
	fmt.Fprintf(w, "file: %s\n", res.File)
	if res.Enabled != nil {
		fmt.Fprintf(w, "enabled: %t\n", *res.Enabled)
	}
	if res.Category != "" {
		fmt.Fprintf(w, "category: %s\n", res.Category)
	}
	if res.TriggerType != "" {
		fmt.Fprintf(w, "trigger_type: %s\n", res.TriggerType)
	}
	if res.ExecutionMode != "" {
		fmt.Fprintf(w, "execution_mode: %s\n", res.ExecutionMode)
	}
	fmt.Fprintf(w, "steps: %d (%d action, %d block, %d automatic, %d manual)\n",
		res.Steps, res.ActionSteps, res.BlockSteps, res.Automatic, res.Manual)
	fmt.Fprintf(w, "relations: %d\n", res.Relations)
	if len(res.Warnings) == 0 {
		fmt.Fprintln(w, "warnings: none")
		return
	}
	fmt.Fprintln(w, "warnings:")
	for _, warning := range res.Warnings {
		fmt.Fprintf(w, "- %s\n", warning)
	}
}
