package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// playbookGetDetail is the structured output of `playbooks get`.
type playbookGetDetail struct {
	Name           string   `json:"name"`
	Identifier     string   `json:"identifier"`
	Enabled        bool     `json:"enabled"`
	Type           string   `json:"type,omitempty"`
	Category       string   `json:"category,omitempty"`
	Priority       any      `json:"priority,omitempty"`
	Description    string   `json:"description,omitempty"`
	Creator        string   `json:"creator,omitempty"`
	ModifiedBy     string   `json:"modifiedBy,omitempty"`
	Version        any      `json:"version,omitempty"`
	Environments   []string `json:"environments,omitempty"`
	TriggerType    string   `json:"triggerType,omitempty"`
	ExecutionMode  string   `json:"executionMode,omitempty"`
	Steps          int      `json:"steps"`
	ActionSteps    int      `json:"actionSteps"`
	ConditionSteps int      `json:"conditionSteps"`
	BlockSteps     int      `json:"blockSteps"`
	Integrations   []string `json:"integrations,omitempty"`
	BlockRefs      []string `json:"blockRefs,omitempty"`
}

func newSOARPlaybookGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name|identifier>",
		Short: "Show a playbook's structure, trigger, steps, and integration dependencies",
		Long: "Fetch a full playbook definition and show its structure: type, trigger,\n" +
			"step count, integration dependencies, and block references. <name> is a\n" +
			"display name (case-insensitive) or a definition identifier UUID.\n\n" +
			"  --json returns the full playbook definition (round-trippable).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier := args[0]
			if !looksLikeUUID(identifier) {
				identifier, err = resolvePlaybookDefinition(ctx, lc, args[0])
				if err != nil {
					return err
				}
			}

			raw, err := lc.GetPlaybook(ctx, identifier)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}

			detail := parsePlaybookDetail(raw)
			detail.Identifier = identifier
			printPlaybookDetail(cmd.OutOrStdout(), detail)
			return nil
		},
	}
	return markJSON(cmd)
}

func parsePlaybookDetail(raw json.RawMessage) playbookGetDetail {
	var doc struct {
		Name                       string            `json:"name"`
		IsEnabled                  *bool             `json:"isEnabled"`
		PlaybookType               any               `json:"playbookType"`
		CategoryName               string            `json:"categoryName"`
		Priority                   any               `json:"priority"`
		Description                string            `json:"description"`
		Creator                    string            `json:"creator"`
		ModifiedBy                 string            `json:"modifiedBy"`
		Version                    any               `json:"version"`
		Environments               []string          `json:"environments"`
		Trigger                    json.RawMessage   `json:"trigger"`
		Steps                      []json.RawMessage `json:"steps"`
		OriginalPlaybookIdentifier string            `json:"originalPlaybookIdentifier"`
	}
	_ = json.Unmarshal(raw, &doc)

	d := playbookGetDetail{
		Name:         doc.Name,
		Enabled:      doc.IsEnabled != nil && *doc.IsEnabled,
		Type:         displayJSONScalar(doc.PlaybookType),
		Category:     doc.CategoryName,
		Priority:     doc.Priority,
		Description:  doc.Description,
		Creator:      doc.Creator,
		ModifiedBy:   doc.ModifiedBy,
		Version:      doc.Version,
		Environments: doc.Environments,
		Steps:        len(doc.Steps),
	}

	if len(doc.Trigger) > 0 && string(doc.Trigger) != "null" {
		var trig playbookTriggerDoc
		if json.Unmarshal(doc.Trigger, &trig) == nil {
			d.TriggerType = displayJSONScalar(trig.Type)
			d.ExecutionMode = displayJSONScalar(trig.ExecutionMode)
		}
	}

	integrations := map[string]bool{}
	var blockNames []string
	for _, stepRaw := range doc.Steps {
		var step playbookStepDoc
		if json.Unmarshal(stepRaw, &step) != nil {
			continue
		}
		stepType := displayJSONScalar(step.Type)
		switch stepType {
		case "ACTION", "Action", "0":
			d.ActionSteps++
		case "CONDITION", "Condition", "4":
			d.ConditionSteps++
		case "BLOCK", "Block":
			blockNames = append(blockNames, step.Name)
		}
		if step.Integration != "" {
			integrations[step.Integration] = true
		}
	}
	d.BlockSteps = len(blockNames)
	d.Integrations = sortedKeys(integrations)
	d.BlockRefs = blockNames

	return d
}

func printPlaybookDetail(w io.Writer, d playbookGetDetail) {
	fmt.Fprintf(w, "%s (%s)\n", orDash(d.Name), d.Identifier)
	fmt.Fprintf(w, "  enabled:      %v\n", d.Enabled)
	if d.Type != "" {
		fmt.Fprintf(w, "  type:         %s\n", d.Type)
	}
	if d.Category != "" {
		fmt.Fprintf(w, "  category:     %s\n", d.Category)
	}
	if d.Priority != nil {
		fmt.Fprintf(w, "  priority:     %v\n", d.Priority)
	}
	if d.Description != "" {
		fmt.Fprintf(w, "  description:  %s\n", truncate(d.Description, 120))
	}
	if d.TriggerType != "" {
		trigger := d.TriggerType
		if d.ExecutionMode != "" {
			trigger += " (" + d.ExecutionMode + ")"
		}
		fmt.Fprintf(w, "  trigger:      %s\n", trigger)
	}
	if len(d.Environments) > 0 {
		fmt.Fprintf(w, "  environments: %s\n", strings.Join(d.Environments, ", "))
	}
	if d.Creator != "" {
		fmt.Fprintf(w, "  creator:      %s\n", d.Creator)
	}
	if d.ModifiedBy != "" {
		fmt.Fprintf(w, "  modified_by:  %s\n", d.ModifiedBy)
	}
	if d.Version != nil {
		fmt.Fprintf(w, "  version:      %v\n", d.Version)
	}

	fmt.Fprintf(w, "  steps:        %d total", d.Steps)
	var parts []string
	if d.ActionSteps > 0 {
		parts = append(parts, fmt.Sprintf("%d action", d.ActionSteps))
	}
	if d.ConditionSteps > 0 {
		parts = append(parts, fmt.Sprintf("%d condition", d.ConditionSteps))
	}
	if d.BlockSteps > 0 {
		parts = append(parts, fmt.Sprintf("%d block", d.BlockSteps))
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w)

	if len(d.Integrations) > 0 {
		fmt.Fprintf(w, "  integrations: %s\n", strings.Join(d.Integrations, ", "))
	}
	if len(d.BlockRefs) > 0 {
		fmt.Fprintf(w, "  block_refs:   %s\n", strings.Join(d.BlockRefs, ", "))
	}
}

func newSOARPlaybookDuplicateCmd() *cobra.Command {
	var (
		newName string
		folder  string
		envs    []string
		dryRun  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "duplicate <name|identifier> --name <new-name>",
		Short: "MUTATING (guarded): duplicate a playbook under a new name",
		Long: "Create a copy of an existing playbook definition. The clone is disabled\n" +
			"by default (SOAR behavior). --name sets the display name of the copy.\n\n" +
			"Options:\n" +
			"  --folder  place the copy in a specific category (name or id)\n" +
			"  --env     override the environments for the copy (repeatable)\n\n" +
			"Uses the modern v1alpha DuplicateWorkflows API; falls back to legacy\n" +
			"DuplicateWorkflow, then export → rename → save on server 500.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier := args[0]
			if !looksLikeUUID(identifier) {
				identifier, err = resolvePlaybookDefinition(ctx, lc, args[0])
				if err != nil {
					return err
				}
			}

			if newName == "" {
				return fmt.Errorf("--name is required")
			}

			var catID int
			if folder != "" {
				catID, _, err = resolveCategory(lc, folder)
				if err != nil {
					return err
				}
			}

			isDry, _ := soarGuard("duplicate playbook "+args[0]+" as "+newName, dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would duplicate %s as %q\n", args[0], newName)
				return nil
			}

			if raw, done, err := duplicateModern(ctx, lc, identifier, newName, catID, envs); done {
				if err != nil {
					return err
				}
				if jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "duplicated")
				return nil
			}

			// Legacy fallback.
			raw, err := lc.DuplicateWorkflow(ctx, map[string]any{
				"workflowIdentifier": identifier,
				"cloneName":          newName,
			})
			if err == nil {
				if catID != 0 {
					_, _ = lc.MoveDefinitionsToCategory(ctx, map[string]any{
						"category":    catID,
						"identifiers": []string{extractLegacyDupID(raw)},
					})
				}
				if jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "duplicated")
				return nil
			}

			var apiErr *legacy.Error
			if !errors.As(err, &apiErr) || apiErr.Status != 500 {
				return err
			}
			fmt.Fprintf(os.Stderr, "DuplicateWorkflow 500 — falling back to export→save\n")
			return duplicateViaExportSave(ctx, lc, identifier, newName, cmd)
		},
	}
	f := cmd.Flags()
	f.StringVar(&newName, "name", "", "display name for the copy (required)")
	f.StringVar(&folder, "folder", "", "target category/folder (name or id)")
	f.StringArrayVar(&envs, "env", nil, "override environments for the copy (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return markJSON(cmd)
}

// duplicateModern tries the v1alpha DuplicateWorkflows endpoint. On success it
// renames the copy to newName (the modern API auto-names "Copy of <original>").
// Returns (raw, true, nil) on success, (nil, true, err) on a hard error, or
// (nil, false, nil) when the modern path is unavailable and the caller should
// fall back to legacy.
func duplicateModern(ctx context.Context, lc *legacy.Client, identifier, newName string, catID int, envs []string) (json.RawMessage, bool, error) {
	mc, err := newSOARClient()
	if err != nil {
		return nil, false, nil
	}

	if envs == nil {
		envs = []string{}
	}
	raw, err := mc.DuplicateWorkflows(ctx, map[string]any{
		"identifiers":  []string{identifier},
		"priority":     0,
		"categoryId":   catID,
		"environments": envs,
	})
	if err != nil {
		return nil, false, nil
	}

	copyID, copyName, err := extractDuplicateResult(raw)
	if err != nil {
		return nil, true, fmt.Errorf("parse duplicate result: %w", err)
	}

	if copyName != newName {
		if err := renameDuplicate(ctx, mc, lc, copyID, newName); err != nil {
			// Rename failed — clean up the auto-named copy.
			_, _ = mc.DeleteWorkflows(ctx, []string{copyID})
			return nil, true, fmt.Errorf("rename duplicate: %w", err)
		}
	}
	return raw, true, nil
}

func extractLegacyDupID(raw json.RawMessage) string {
	var doc struct {
		Identifier string `json:"identifier"`
	}
	_ = json.Unmarshal(raw, &doc)
	return doc.Identifier
}

func extractDuplicateResult(raw json.RawMessage) (id, name string, err error) {
	var envelope struct {
		Payload []struct {
			Identifier string `json:"identifier"`
			Name       string `json:"name"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return "", "", err
	}
	if len(envelope.Payload) == 0 {
		return "", "", fmt.Errorf("empty payload")
	}
	return envelope.Payload[0].Identifier, envelope.Payload[0].Name, nil
}

func renameDuplicate(ctx context.Context, mc *soar.Client, lc *legacy.Client, copyID, newName string) error {
	// Fetch the full definition, change the name, save.
	raw, err := mc.GetWorkflowFullInfo(ctx, copyID)
	if err != nil {
		raw, err = lc.GetPlaybook(ctx, copyID)
		if err != nil {
			return err
		}
	}
	var def map[string]any
	if err := json.Unmarshal(raw, &def); err != nil {
		return err
	}
	def["name"] = newName
	_, err = mc.SaveWorkflowDefinitions(ctx, def)
	if err != nil {
		modified, _ := json.Marshal(def)
		_, err = lc.SavePlaybook(ctx, modified)
	}
	return err
}

// duplicateViaExportSave clones a playbook by exporting its save-compatible
// definition, changing the name and identifiers, then saving as a new disabled
// playbook. Used as fallback when the native DuplicateWorkflow API 500s.
func duplicateViaExportSave(ctx context.Context, lc *legacy.Client, identifier, newName string, cmd *cobra.Command) error {
	// Check name collision first.
	if _, err := lc.GetPlaybookByName(ctx, newName, false); err == nil {
		return fmt.Errorf("a playbook named %q already exists", newName)
	}

	raw, err := lc.GetPlaybook(ctx, identifier)
	if err != nil {
		return fmt.Errorf("export source playbook: %w", err)
	}

	// Modify: new name, new identifier, disabled.
	var def map[string]any
	if err := json.Unmarshal(raw, &def); err != nil {
		return fmt.Errorf("decode playbook: %w", err)
	}
	def["name"] = newName
	def["isEnabled"] = false

	newID, err := newRandomUUID()
	if err != nil {
		return err
	}
	def["originalPlaybookIdentifier"] = newID

	modified, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("encode modified playbook: %w", err)
	}

	out, err := lc.SavePlaybook(ctx, modified)
	if err != nil {
		return fmt.Errorf("save clone: %w", err)
	}
	if jsonOut {
		return writeRawJSON(os.Stdout, out)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "duplicated (via export→save fallback)")
	return nil
}

// isEnumTypeMismatch detects the modern v1alpha 400 when a playbook
// definition carries numeric enum fields (e.g. from a zip import) that the
// API expects as strings. The legacy path accepts both — so this error is
// silently swallowed during preferModern fallback.
func isEnumTypeMismatch(err error) bool {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == 400 && strings.Contains(apiErr.Body, "Expected string TokenType for enum")
}

// wrapPlaybookRunError translates opaque SOAR 500s from run/rerun into
// actionable messages. A 500 on a COMPLETED case means the workflow
// already finished.
func wrapPlaybookRunError(err error) error {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.Status == 500 {
		bodyLower := strings.ToLower(apiErr.Body)
		if strings.Contains(bodyLower, "completed") || strings.Contains(bodyLower, "workflow") {
			return fmt.Errorf("playbook run/rerun failed (likely a completed case workflow):\n"+
				"  the case workflow may have already completed — create a simulation\n"+
				"  case (`cases simulation create`) or use a new case\n"+
				"  (original: %w)", err)
		}
	}
	return err
}

// wrapPlaybookSaveError translates opaque SOAR 400s from push/deploy into
// actionable messages. A 400 "refresh the screen" means the local
// export is based on a superseded version.
func wrapPlaybookSaveError(err error) error {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.Status == 400 {
		bodyLower := strings.ToLower(apiErr.Body)
		if strings.Contains(bodyLower, "refresh") || strings.Contains(bodyLower, "new version") {
			return fmt.Errorf("playbook save rejected — local file based on a superseded version:\n"+
				"  re-export the playbook (`playbooks export --name <name>`), re-apply\n"+
				"  your edit, then push again\n"+
				"  (original: %w)", err)
		}
	}
	return err
}

func looksLikeUUID(s string) bool {
	if len(s) < 32 {
		return false
	}
	dashes := 0
	for _, c := range s {
		switch {
		case c == '-':
			dashes++
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return dashes >= 4
}
