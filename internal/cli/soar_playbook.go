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
		newSOARPlaybookListCmd(),
		newSOARPlaybookDeleteCmd(),
		newSOARPlaybookDeployCmd(),
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
		newSOARPlaybookGenerateCmd(),
		newSOARPlaybookGenerateStatusCmd(),
	)
	return cmd
}

func newSOARPlaybookListCmd() *cobra.Command {
	var (
		enabledOnly bool
		types       []string
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
			for _, card := range cards {
				if enabledOnly && !card.IsEnabled {
					continue
				}
				rows = append(rows, soarPlaybookListRow{
					Name:       card.Name,
					Enabled:    card.IsEnabled,
					Category:   card.CategoryName,
					ID:         card.ID.String(),
					Identifier: card.Identifier,
				})
			}
			sort.Slice(rows, func(i, j int) bool {
				return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
			})
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
	return markJSON(cmd)
}

func newSOARPlaybookDeleteCmd() *cobra.Command {
	var (
		name       string
		identifier string
		fromFile   string
		dryRun     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "delete (--name <playbook> | --identifier <uuid>[,uuid,...] | --from-file <path>)",
		Short: "MUTATING (guarded): delete one or more playbooks permanently",
		Long: "Delete playbook definitions by name or identifier UUID.\n\n" +
			"Single: --name resolves to the definition id via the live playbook list\n" +
			"(case-insensitive exact match).\n\n" +
			"Batch: --identifier accepts comma-separated UUIDs, or --from-file reads\n" +
			"one UUID per line (blank lines and #-comments skipped). A batch delete\n" +
			"uses a single API call and reports per-playbook success/failure.\n\n" +
			"Guarded: dry-run by default, --yes to apply. Deleting a playbook that\n" +
			"is attached to a case stops its execution — irreversible.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			identifier = strings.TrimSpace(identifier)
			fromFile = strings.TrimSpace(fromFile)

			ids, err := collectPlaybookDeleteIDs(name, identifier, fromFile)
			if err != nil {
				return err
			}

			lc, lerr := newSOARLegacyClient()
			if lerr != nil {
				return lerr
			}
			ctx := baseContext()

			// Resolve name → identifier for the single-name path.
			if name != "" {
				resolved, rerr := resolvePlaybookDefinition(ctx, lc, name)
				if rerr != nil {
					return rerr
				}
				ids = []string{resolved}
			}

			label := formatDeleteLabel(name, ids)
			dr, ay := soarGuard("playbook delete "+label, dryRun, yes)
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN: would delete %d playbook(s): %s\n", len(ids), label)
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to delete without confirmation (pass --yes). Aborted.")
				return nil
			}

			// For batch (>1), don't use preferModern — a partial success
			// must not trigger a legacy fallback that re-deletes succeeded items.
			if len(ids) == 1 {
				return preferModern("soar playbook delete",
					func() error {
						mc, merr := newSOARClient()
						if merr != nil {
							return merr
						}
						_, merr = mc.DeleteWorkflows(ctx, ids)
						if merr != nil {
							return merr
						}
						fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", ids[0])
						return nil
					},
					func() error {
						body := map[string]any{"identifiers": ids}
						_, lerr := lc.DeleteWorkflows(ctx, body)
						if lerr != nil {
							return lerr
						}
						fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", ids[0])
						return nil
					},
				)
			}
			// Batch: try modern first; on transport error fall back to legacy.
			// On a successful API response, report per-item results and don't retry.
			mc, merr := newSOARClient()
			if merr == nil {
				raw, merr := mc.DeleteWorkflows(ctx, ids)
				if merr == nil {
					return reportBatchDelete(cmd.OutOrStdout(), ids, raw)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "soar playbook delete: modern path failed (%v) — falling back to legacy\n", merr)
			}
			body := map[string]any{"identifiers": ids}
			raw, lerr := lc.DeleteWorkflows(ctx, body)
			if lerr != nil {
				return lerr
			}
			return reportBatchDelete(cmd.OutOrStdout(), ids, raw)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved to its id via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition UUID(s), comma-separated")
	f.StringVar(&fromFile, "from-file", "", "file with one playbook UUID per line")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("name", "identifier", "from-file")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func collectPlaybookDeleteIDs(name, identifier, fromFile string) ([]string, error) {
	if name != "" {
		return nil, nil // resolved later
	}
	if fromFile != "" {
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("reading --from-file: %w", err)
		}
		var ids []string
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			ids = append(ids, line)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--from-file %q contains no identifiers", fromFile)
		}
		return ids, nil
	}
	if identifier != "" {
		var ids []string
		for id := range strings.SplitSeq(identifier, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--identifier is empty")
		}
		return ids, nil
	}
	return nil, fmt.Errorf("pass --name, --identifier, or --from-file")
}

func formatDeleteLabel(name string, ids []string) string {
	if name != "" {
		return fmt.Sprintf("%q", name)
	}
	if len(ids) <= 3 {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s, ... (%d total)", strings.Join(ids[:2], ", "), len(ids))
}

func reportBatchDelete(w io.Writer, ids []string, raw json.RawMessage) error {
	if len(ids) == 1 {
		fmt.Fprintf(w, "deleted playbook %s\n", ids[0])
		return nil
	}
	var resp struct {
		Results []struct {
			Identifier   string `json:"identifier"`
			Failed       bool   `json:"failed"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Results) == 0 {
		fmt.Fprintf(w, "deleted %d playbook(s)\n", len(ids))
		return nil
	}
	var ok, fail int
	for _, r := range resp.Results {
		if r.Failed {
			fmt.Fprintf(w, "FAILED  %s: %s\n", r.Identifier, r.ErrorMessage)
			fail++
		} else {
			fmt.Fprintf(w, "deleted %s\n", r.Identifier)
			ok++
		}
	}
	if fail > 0 {
		return fmt.Errorf("%d of %d playbook(s) failed to delete", fail, ok+fail)
	}
	return nil
}

func newSOARPlaybookDeployCmd() *cobra.Command {
	var (
		name       string
		identifier string
		enable     bool
		disable    bool
		dryRun     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "deploy (--name <playbook> | --identifier <uuid>) --enable|--disable",
		Short: "MUTATING (guarded): enable or disable a playbook",
		Long: "Toggle a playbook's isEnabled state. Reads the full definition, flips the\n" +
			"flag, and saves via SaveWorkflowDefinitions (the only API path — this mints a\n" +
			"new version). Guarded: dry-run by default, --yes to apply.\n\n" +
			"Mirrors `rules-deploy` for consistency.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !enable && !disable {
				return fmt.Errorf("pass --enable or --disable")
			}
			wantEnabled := enable

			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier, err = resolvePlaybookSelector(ctx, lc, name, identifier)
			if err != nil {
				return err
			}

			// Read the full definition.
			raw, err := lc.GetWorkflowFullInfo(ctx, identifier)
			if err != nil {
				return err
			}
			var def map[string]any
			if err := json.Unmarshal(raw, &def); err != nil {
				return fmt.Errorf("decode playbook definition: %w", err)
			}

			currentEnabled, _ := def["isEnabled"].(bool)
			pbName, _ := def["name"].(string)
			if pbName == "" {
				pbName = identifier
			}

			toggle := "disable"
			if wantEnabled {
				toggle = "enable"
			}

			if currentEnabled == wantEnabled {
				fmt.Fprintf(os.Stdout, "playbook %q is already %sd — nothing to do.\n", pbName, toggle)
				return nil
			}

			action := fmt.Sprintf("playbook deploy %s → %s", pbName, toggle)
			dr, ay := soarGuard(action, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Playbook: %q (%s)\n", pbName, identifier)
			fmt.Fprintf(os.Stdout, "  isEnabled: %v → %v (mints a new version)\n", currentEnabled, wantEnabled)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}

			def["isEnabled"] = wantEnabled
			return preferModern("soar playbook deploy",
				func() error {
					mc, merr := newSOARClient()
					if merr != nil {
						return merr
					}
					if _, merr = mc.SaveWorkflowDefinitions(ctx, def); merr != nil {
						return merr
					}
					fmt.Fprintf(os.Stdout, "playbook %q %sd.\n", pbName, toggle)
					return nil
				},
				func() error {
					if _, lerr := lc.SaveWorkflowDefinitions(ctx, def); lerr != nil {
						return lerr
					}
					fmt.Fprintf(os.Stdout, "playbook %q %sd.\n", pbName, toggle)
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved to its id via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition UUID (overrides --name)")
	f.BoolVar(&enable, "enable", false, "set isEnabled=true")
	f.BoolVar(&disable, "disable", false, "set isEnabled=false")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
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
	fmt.Fprintln(w, "ENABLED\tNAME\tCATEGORY\tIDENTIFIER")
	for _, row := range rows {
		fmt.Fprintf(w, "%t\t%s\t%s\t%s\n", row.Enabled, row.Name, row.Category, row.Identifier)
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
