package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
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
		Use:   "playbook",
		Short: "Discover and validate SOAR playbooks",
		Long: "Discover live SOAR playbooks and validate exported playbook JSON before\n" +
			"a guarded save. This is a SecOps interaction surface: local files are\n" +
			"only review/preflight artifacts before SecOps validates and runs them.",
	}
	cmd.AddCommand(
		newSOARPlaybookListCmd(),
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
	return cmd
}

func newSOARPlaybookTestCasesCmd() *cobra.Command {
	var (
		searchTerm string
		envs       []string
		page       int
		pageSize   int
	)
	cmd := &cobra.Command{
		Use:   "test-cases",
		Short: "List SecOps playbook debug test cases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := testCasesBody(searchTerm, envs, page, pageSize)
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetTestCases(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "test_cases", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&searchTerm, "search", "", "optional test-case search term")
	f.StringArrayVar(&envs, "environment", nil, "environment filter (repeatable)")
	f.IntVar(&page, "page", 0, "requested page")
	f.IntVar(&pageSize, "page-size", 50, "page size")
	return cmd
}

func newSOARPlaybookRunCmd() *cobra.Command {
	var (
		caseID      int
		name        string
		identifier  string
		alert       string
		alertGroup  string
		automatic   bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "run --case-id N (--name <playbook> | --identifier <uuid>)",
		Short: "GUARDED: attach and run a playbook on an explicit case/alert",
		Long: "Attach a live SOAR playbook to one explicit case, optionally scoped to\n" +
			"one alert. Dry-run is the default and prints the SecOps request body;\n" +
			"pass --yes to ask SecOps to attach/run it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := playbookRunBody(caseID, name, identifier, alertGroup, alert, automatic)
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook run", dryRun, yes)
			if err := emitPlaybookMutationPreview("RUN SOAR playbook on case/alert", body, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitPlaybookMutationJSON("playbook run", body, dr, false, nil)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.AttachWorkflowToCase(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitPlaybookMutationJSON("playbook run", body, dr, true, raw)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Playbook attach/run requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id to run against (required)")
	f.StringVar(&name, "name", "", "playbook name")
	f.StringVar(&identifier, "identifier", "", "original workflow definition identifier")
	f.StringVar(&alertGroup, "alert-group", "", "optional alert group identifier")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	f.BoolVar(&automatic, "automatic", true, "allow automatic playbook actions to run")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("case-id")
	return cmd
}

func newSOARPlaybookDebugCmd() *cobra.Command {
	var (
		file        string
		testCaseID  int
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "debug --file <playbook.json> --test-case-id N",
		Short: "GUARDED: run a playbook definition in SecOps debug mode",
		Long: "Run an exported playbook definition in SecOps debug/simulation mode\n" +
			"against an explicit SecOps test case. Dry-run is the default; pass --yes\n" +
			"to ask SecOps to start the debug run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := playbookDebugBody(file, testCaseID)
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook debug", dryRun, yes)
			if err := emitPlaybookMutationPreview("DEBUG SOAR playbook", map[string]any{
				"file":       file,
				"testCaseId": testCaseID,
			}, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitPlaybookMutationJSON("playbook debug", map[string]any{
					"file":       file,
					"testCaseId": testCaseID,
				}, dr, false, nil)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXRunInDebug(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitPlaybookMutationJSON("playbook debug", map[string]any{
					"file":       file,
					"testCaseId": testCaseID,
				}, dr, true, raw)
			}
			printPlaybookDebugResponse(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "playbook JSON file to debug (required)")
	f.IntVar(&testCaseID, "test-case-id", 0, "SecOps test case id (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("test-case-id")
	return cmd
}

func newSOARPlaybookDebugStepDataCmd() *cobra.Command {
	var (
		stepIdentifier string
		alert          string
	)
	cmd := &cobra.Command{
		Use:   "debug-step-data --step-identifier <uuid>",
		Short: "Read simulated case data for a debug step",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := debugStepDataBody(stepIdentifier, alert)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetDebugStepCaseData(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "debug_step_case_data", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&stepIdentifier, "step-identifier", "", "original step identifier (required)")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	_ = cmd.MarkFlagRequired("step-identifier")
	return cmd
}

func newSOARPlaybookSimulationEnrichmentCmd() *cobra.Command {
	var (
		testCaseID         int
		stepIdentifier     string
		workflowIdentifier string
	)
	cmd := &cobra.Command{
		Use:   "simulation-enrichment --test-case-id N --step-identifier <uuid> --workflow-identifier <uuid>",
		Short: "Read enrichment data for a playbook simulation step",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := simulationEnrichmentBody(testCaseID, stepIdentifier, workflowIdentifier)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetSimulationEnrichment(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "simulation_enrichment_records", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&testCaseID, "test-case-id", 0, "SecOps test case id (required)")
	f.StringVar(&stepIdentifier, "step-identifier", "", "original step identifier (required)")
	f.StringVar(&workflowIdentifier, "workflow-identifier", "", "original workflow identifier (required)")
	_ = cmd.MarkFlagRequired("test-case-id")
	_ = cmd.MarkFlagRequired("step-identifier")
	_ = cmd.MarkFlagRequired("workflow-identifier")
	return cmd
}

func newSOARPlaybookPendingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Read pending SOAR playbook steps",
		Long: "Read pending playbook steps assigned to the current user. These\n" +
			"commands are read-only discovery helpers before any guarded step action.",
	}
	cmd.AddCommand(
		newSOARPlaybookPendingCountCmd(),
		newSOARPlaybookPendingListCmd(),
		newSOARPlaybookPendingGetCmd(),
	)
	return cmd
}

func newSOARPlaybookPendingCountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "count",
		Short: "Count pending playbook steps for the current user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetPendingStepsCountForUser(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printPendingStepCount(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	return cmd
}

func newSOARPlaybookPendingListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending playbook steps for the current user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetPendingStepsUserRelated(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "pending steps", raw)
			return nil
		},
	}
	return cmd
}

func newSOARPlaybookPendingGetCmd() *cobra.Command {
	var (
		caseID             int
		alertGroup         string
		workflowIdentifier string
	)
	cmd := &cobra.Command{
		Use:   "get --case-id N",
		Short: "Read one pending playbook step for a case/workflow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := pendingStepBody(caseID, alertGroup, workflowIdentifier)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetPendingStep(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "pending step records", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&alertGroup, "alert-group", "", "optional alert group identifier")
	f.StringVar(&workflowIdentifier, "workflow-identifier", "", "optional original workflow definition identifier")
	_ = cmd.MarkFlagRequired("case-id")
	return cmd
}

func newSOARPlaybookRerunCmd() *cobra.Command {
	var (
		caseID      int
		name        string
		identifier  string
		alert       string
		alertGroup  string
		automatic   bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "rerun --case-id N (--name <playbook> | --identifier <uuid>)",
		Short: "GUARDED: rerun a playbook on an explicit case/alert",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := playbookRunBody(caseID, name, identifier, alertGroup, alert, automatic)
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook rerun", dryRun, yes)
			if err := emitPlaybookMutationPreview("RERUN SOAR playbook on case/alert", body, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitPlaybookMutationJSON("playbook rerun", body, dr, false, nil)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXRerun(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitPlaybookMutationJSON("playbook rerun", body, dr, true, raw)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Playbook rerun requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id to rerun against (required)")
	f.StringVar(&name, "name", "", "playbook name")
	f.StringVar(&identifier, "identifier", "", "original workflow definition identifier")
	f.StringVar(&alertGroup, "alert-group", "", "optional alert group identifier")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	f.BoolVar(&automatic, "automatic", true, "allow automatic playbook actions to run")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("case-id")
	return cmd
}

func newSOARPlaybookRerunBlockCmd() *cobra.Command {
	var (
		caseID      int
		name        string
		identifier  string
		alert       string
		alertGroup  string
		inputsFile  string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "rerun-block --case-id N (--name <block> | --identifier <uuid>)",
		Short: "GUARDED: rerun a nested playbook block on an explicit case/alert",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := playbookBlockRunBody(caseID, name, identifier, alertGroup, alert, inputsFile)
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook rerun-block", dryRun, yes)
			if err := emitPlaybookMutationPreview("RERUN SOAR playbook block on case/alert", body, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitPlaybookMutationJSON("playbook rerun-block", body, dr, false, nil)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXRerunBlock(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitPlaybookMutationJSON("playbook rerun-block", body, dr, true, raw)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Playbook block rerun requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id to rerun against (required)")
	f.StringVar(&name, "name", "", "nested block playbook name")
	f.StringVar(&identifier, "identifier", "", "original nested workflow definition identifier")
	f.StringVar(&alertGroup, "alert-group", "", "optional alert group identifier")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	f.StringVar(&inputsFile, "inputs", "", "optional JSON array of block inputParameters")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("case-id")
	return cmd
}

func newSOARPlaybookSummaryCmd() *cobra.Command {
	var (
		caseID         int
		alert          string
		definition     string
		playbook       string
		fetchSteps     bool
		collapseBlocks bool
		showErrors     bool
	)
	cmd := &cobra.Command{
		Use:   "summary --case-id N --playbook <name>",
		Short: "Read a playbook run summary for a case (surfaces faulted steps)",
		Long: "Fetch a playbook workflow-instance summary and surface its FAULTED steps —\n" +
			"each failed step's action, error message, and a Cloud Logging deep-link — so a\n" +
			"failed run is triaged in-tool without digging through the raw payload.\n\n" +
			"The easy form needs only the case id and a playbook NAME:\n" +
			"  secopsctl soar playbook summary --case-id 123 --playbook \"My Playbook\"\n" +
			"`--playbook` is resolved to its definition id via `soar playbook list`, and the\n" +
			"alert identifier is read from the case automatically (use `--alert` when a case\n" +
			"has more than one alert, or `--definition` to pass the id directly). Prefers the\n" +
			"v1alpha SOAR-host path and falls back to the legacy API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			// Resolve the friendly inputs to the opaque selectors the API needs:
			// a playbook NAME → its definition id, and (when --alert is omitted) the
			// case's single alert identifier.
			if strings.TrimSpace(definition) == "" && strings.TrimSpace(playbook) != "" {
				lc, lerr := newSOARLegacyClient()
				if lerr != nil {
					return lerr
				}
				if definition, lerr = resolvePlaybookDefinition(baseContext(), lc, playbook); lerr != nil {
					return lerr
				}
			}
			if strings.TrimSpace(alert) == "" {
				lc, lerr := newSOARLegacyClient()
				if lerr != nil {
					return lerr
				}
				if alert, lerr = resolveCaseAlert(baseContext(), lc, caseID); lerr != nil {
					return lerr
				}
			}
			if strings.TrimSpace(definition) == "" {
				return fmt.Errorf("select the run: pass --playbook <name> or --definition <id>")
			}
			legacyBody, err := workflowSummaryBody(caseID, alert, definition, fetchSteps, collapseBlocks)
			if err != nil {
				return err
			}
			// The v1alpha legacyPlaybooks path expects caseId as a string (matching the
			// console request); the legacy external API takes the int form.
			modernBody, _ := workflowSummaryBody(caseID, alert, definition, fetchSteps, collapseBlocks)
			modernBody["caseId"] = strconv.Itoa(caseID)
			render := func(raw json.RawMessage) error {
				if jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				printWorkflowSummary(cmd.OutOrStdout(), raw, showErrors)
				return nil
			}
			return preferModern("soar playbook summary",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.GetWorkflowInstanceSummary(baseContext(), modernBody)
					if err != nil {
						return err
					}
					return render(raw)
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.GetWorkflowInstanceSummary(baseContext(), legacyBody)
					if err != nil {
						return err
					}
					return render(raw)
				},
			)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&playbook, "playbook", "", "playbook name — resolved to its definition id via `soar playbook list`")
	f.StringVar(&alert, "alert", "", "alert identifier (auto-resolved from the case when omitted)")
	f.StringVar(&definition, "definition", "", "playbook definition id (overrides --playbook)")
	f.BoolVar(&fetchSteps, "fetch-steps", true, "include step details")
	f.BoolVar(&collapseBlocks, "collapse-blocks", false, "collapse nested block details")
	f.BoolVar(&showErrors, "show-errors", false, "print full faulted-step error messages (default truncates)")
	_ = cmd.MarkFlagRequired("case-id")
	return cmd
}

func newSOARPlaybookResultsCmd() *cobra.Command {
	var workflowInstanceID int
	cmd := &cobra.Command{
		Use:   "results --workflow-instance-id N",
		Short: "Read action results for a workflow instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workflowInstanceID <= 0 {
				return fmt.Errorf("--workflow-instance-id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetActionResultsOfWFId(baseContext(), workflowInstanceID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printActionResultsSummary(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&workflowInstanceID, "workflow-instance-id", 0, "workflow instance id (required)")
	_ = cmd.MarkFlagRequired("workflow-instance-id")
	return cmd
}

func newSOARPlaybookResultCmd() *cobra.Command {
	var (
		caseID int
		id     string
	)
	cmd := &cobra.Command{
		Use:   "result --case-id N --action-result-id <id>",
		Short: "Read one action result from a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--action-result-id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ResourceGetActionResultsById(baseContext(), caseID, id)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printSingleActionResultSummary(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&id, "action-result-id", "", "action result id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	_ = cmd.MarkFlagRequired("action-result-id")
	return cmd
}

func newSOARPlaybookPythonLogsCmd() *cobra.Command {
	var (
		filter    string
		pageToken string
		sortOrder string
		pageSize  int
	)
	cmd := &cobra.Command{
		Use:   "python-logs",
		Short: "Read Python execution logs from SecOps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.CloudLoggingGetPythonLogs(baseContext(), pythonLogsBody(filter, pageToken, sortOrder, pageSize))
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "python log records", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "SecOps log filter expression")
	f.IntVar(&pageSize, "page-size", 50, "maximum records to request")
	f.StringVar(&pageToken, "page-token", "", "page token from a previous response")
	f.StringVar(&sortOrder, "sort-order", "", "SecOps sort order")
	return cmd
}

func pythonLogsBody(filter, pageToken, sortOrder string, pageSize int) map[string]any {
	body := map[string]any{}
	filter = strings.TrimSpace(filter)
	if filter != "" {
		body["filter"] = filter
	}
	pageToken = strings.TrimSpace(pageToken)
	if pageToken != "" {
		body["pageToken"] = pageToken
	}
	sortOrder = strings.TrimSpace(sortOrder)
	if sortOrder != "" {
		body["sortOrder"] = sortOrder
	}
	if pageSize > 0 {
		body["pageSize"] = pageSize
	}
	return body
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

func playbookRunBody(caseID int, name, identifier, alertGroup, alert string, automatic bool) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	name = strings.TrimSpace(name)
	identifier = strings.TrimSpace(identifier)
	if name == "" && identifier == "" {
		return nil, fmt.Errorf("--name or --identifier is required")
	}
	body := map[string]any{
		"cyberCaseId":        caseID,
		"shouldRunAutomatic": automatic,
	}
	if name != "" {
		body["wfName"] = name
	}
	if identifier != "" {
		body["originalWorkflowDefinitionIdentifier"] = identifier
	}
	if alertGroup = strings.TrimSpace(alertGroup); alertGroup != "" {
		body["alertGroupIdentifier"] = alertGroup
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	return body, nil
}

func playbookBlockRunBody(caseID int, name, identifier, alertGroup, alert, inputsFile string) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	name = strings.TrimSpace(name)
	identifier = strings.TrimSpace(identifier)
	if name == "" && identifier == "" {
		return nil, fmt.Errorf("--name or --identifier is required")
	}
	body := map[string]any{"cyberCaseId": caseID}
	if name != "" {
		body["wfName"] = name
	}
	if identifier != "" {
		body["originalWorkflowDefinitionIdentifier"] = identifier
	}
	if alertGroup = strings.TrimSpace(alertGroup); alertGroup != "" {
		body["alertGroupIdentifier"] = alertGroup
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	if strings.TrimSpace(inputsFile) != "" {
		inputs, err := readJSONArrayFile(inputsFile)
		if err != nil {
			return nil, err
		}
		body["inputParameters"] = inputs
	}
	return body, nil
}

func playbookDebugBody(file string, testCaseID int) (map[string]any, error) {
	if testCaseID <= 0 {
		return nil, fmt.Errorf("--test-case-id is required")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := legacy.ValidatePlaybookForSave(json.RawMessage(raw)); err != nil {
		return nil, err
	}
	return map[string]any{
		"workflow":   json.RawMessage(raw),
		"testCaseId": testCaseID,
	}, nil
}

func testCasesBody(searchTerm string, envs []string, page, pageSize int) map[string]any {
	body := map[string]any{
		"searchTerm":   strings.TrimSpace(searchTerm),
		"environments": []string{},
	}
	if page >= 0 {
		body["requestedPage"] = page
	}
	if pageSize > 0 {
		body["pageSize"] = pageSize
	}
	if envs = trimNonEmpty(envs); len(envs) > 0 {
		body["environments"] = envs
	}
	return body
}

func debugStepDataBody(stepIdentifier, alert string) (map[string]any, error) {
	stepIdentifier = strings.TrimSpace(stepIdentifier)
	if stepIdentifier == "" {
		return nil, fmt.Errorf("--step-identifier is required")
	}
	body := map[string]any{"stepOriginalIdentifier": stepIdentifier}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	return body, nil
}

func simulationEnrichmentBody(testCaseID int, stepIdentifier, workflowIdentifier string) (map[string]any, error) {
	if testCaseID <= 0 {
		return nil, fmt.Errorf("--test-case-id is required")
	}
	stepIdentifier = strings.TrimSpace(stepIdentifier)
	workflowIdentifier = strings.TrimSpace(workflowIdentifier)
	if stepIdentifier == "" {
		return nil, fmt.Errorf("--step-identifier is required")
	}
	if workflowIdentifier == "" {
		return nil, fmt.Errorf("--workflow-identifier is required")
	}
	return map[string]any{
		"testCaseId":                 testCaseID,
		"originalStepIdentifier":     stepIdentifier,
		"originalWorkflowIdentifier": workflowIdentifier,
	}, nil
}

func pendingStepBody(caseID int, alertGroup, workflowIdentifier string) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	body := map[string]any{"caseId": caseID}
	if alertGroup = strings.TrimSpace(alertGroup); alertGroup != "" {
		body["alertGroupIdentifier"] = alertGroup
	}
	if workflowIdentifier = strings.TrimSpace(workflowIdentifier); workflowIdentifier != "" {
		body["originalWorkflowDefinitionIdentifier"] = workflowIdentifier
	}
	return body, nil
}

// resolvePlaybookDefinition maps a playbook display name to its definition id via
// the live playbook list (case-insensitive exact match), so callers pass a name
// instead of the opaque GUID. It errors clearly on no/ambiguous match.
func resolvePlaybookDefinition(ctx context.Context, lc *legacy.Client, name string) (string, error) {
	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	var ids []string
	for _, c := range cards {
		// Skip a blank identifier so a name match can't resolve to "" (which would
		// then surface as a generic 500 from the summary call).
		if id := strings.TrimSpace(c.Identifier); id != "" && strings.EqualFold(strings.TrimSpace(c.Name), name) {
			ids = append(ids, id)
		}
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no playbook named %q (see `soar playbook list`)", name)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("%d playbooks named %q — pass --definition <id> to disambiguate", len(ids), name)
	}
}

// resolveCaseAlert returns the alert identifier a workflow-instance summary needs —
// alerts[].additionalProperties.alertGroupIdentifier from the full case detail. A
// single-alert case resolves automatically; a multi-alert case errors and lists
// the alerts so the caller can pass --alert.
func resolveCaseAlert(ctx context.Context, lc *legacy.Client, caseID int) (string, error) {
	raw, err := lc.GetCaseFullDetails(ctx, caseID)
	if err != nil {
		return "", err
	}
	var cd struct {
		Alerts []struct {
			Name                 string `json:"name"`
			AdditionalProperties struct {
				AlertGroupIdentifier string `json:"alertGroupIdentifier"`
			} `json:"additionalProperties"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &cd); err != nil {
		return "", fmt.Errorf("parse case %d details: %w", caseID, err)
	}
	var ids, names []string
	for _, a := range cd.Alerts {
		if id := strings.TrimSpace(a.AdditionalProperties.AlertGroupIdentifier); id != "" {
			ids = append(ids, id)
			names = append(names, a.Name)
		}
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("case %d exposes no alert identifier — pass --alert <id>", caseID)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("case %d has %d alerts (%s) — pass --alert <id>", caseID, len(ids), strings.Join(names, ", "))
	}
}

func workflowSummaryBody(caseID int, alert, definition string, fetchSteps, collapseBlocks bool) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	body := map[string]any{
		"caseId":           caseID,
		"shouldFetchSteps": fetchSteps,
		"collapseBlocks":   collapseBlocks,
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	if definition = strings.TrimSpace(definition); definition != "" {
		body["definitionIdentifier"] = definition
	}
	return body, nil
}

func readJSONArrayFile(path string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if firstJSONByte(json.RawMessage(raw)) != '[' {
		return nil, fmt.Errorf("%s must contain a JSON array", path)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return json.RawMessage(raw), nil
}

func emitPlaybookMutationPreview(action string, body map[string]any, dryRun, assumeYes bool) error {
	if jsonOut {
		return nil
	}
	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SOAR playbook action against a PRODUCTION tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	pretty, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Request body:\n%s\n\n", pretty)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to act without confirmation (pass --yes). Aborted.")
	}
	return nil
}

func trimNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func emitPlaybookMutationJSON(action string, request map[string]any, dryRun, applied bool, response json.RawMessage) error {
	if !jsonOut {
		return nil
	}
	return emitJSON(struct {
		Action   string          `json:"action"`
		Request  map[string]any  `json:"request"`
		DryRun   bool            `json:"dry_run"`
		Applied  bool            `json:"applied"`
		OK       bool            `json:"ok"`
		Response json.RawMessage `json:"response,omitempty"`
	}{Action: action, Request: request, DryRun: dryRun, Applied: applied, OK: true, Response: response})
}

func printPlaybookDebugResponse(w io.Writer, raw json.RawMessage) {
	m, ok := rawJSONObject(raw)
	if !ok {
		printGenericItemsSummary(w, "debug response records", raw)
		return
	}
	fmt.Fprintln(w, "debug_run:")
	printJSONField(w, m, "workflowInstanceId", "workflow_instance_id")
	printJSONField(w, m, "newTestCaseId", "new_test_case_id")
	printJSONField(w, m, "triggerMatches", "trigger_matches")
	printJSONField(w, m, "newWorkflowIdentifier", "new_workflow_identifier")
	if hasJSONValue(m, "alertGroupIdentifier") {
		fmt.Fprintln(w, "alert_group_identifier: present")
	}
	if hasJSONValue(m, "alertIdentifier") {
		fmt.Fprintln(w, "alert_identifier: present")
	}
}

// faultedStep is the subset of a faulted workflow step we surface: the action
// that failed, its status, the runtime error message (a Python traceback, for a
// script action), and a Cloud Logging deep-link to the raw step logs.
type faultedStep struct {
	ActionName              string `json:"actionName"`
	Name                    string `json:"name"`
	Status                  string `json:"status"`
	Message                 string `json:"message"`
	IntegrationInstanceName string `json:"integrationInstanceName"`
	LogsExplorerURL         string `json:"logsExplorerUrl"`
}

func printWorkflowSummary(w io.Writer, raw json.RawMessage, showErrors bool) {
	m, ok := rawJSONObject(raw)
	if !ok {
		printGenericItemsSummary(w, "workflow summary records", raw)
		return
	}
	fmt.Fprintln(w, "workflow_summary:")
	// Step collections are large arrays — print their counts, not the full dump.
	for _, kv := range []struct{ key, label string }{
		{"completedSteps", "completed"},
		{"faultedSteps", "faulted"},
		{"retryPendingSteps", "retry_pending"},
		{"usedIntegrations", "used_integrations"},
	} {
		if rawItems, ok := m[kv.key]; ok {
			fmt.Fprintf(w, "  %s: %d\n", kv.label, jsonArrayLen(rawItems))
		}
	}
	// Scalars.
	for _, key := range []string{"totalPendingActionSteps", "executionTimeInMs"} {
		printJSONField(w, m, key, snakeCase(key))
	}
	printFaultedSteps(w, m["faultedSteps"], showErrors)
}

// printFaultedSteps expands each faulted step with its action, error message, and
// Logs Explorer link — the point of inspecting a failed run. The error message is
// collapsed to one truncated line unless showErrors is set.
func printFaultedSteps(w io.Writer, rawFaulted json.RawMessage, showErrors bool) {
	if len(rawFaulted) == 0 {
		return
	}
	var steps []faultedStep
	if err := json.Unmarshal(rawFaulted, &steps); err != nil || len(steps) == 0 {
		return
	}
	fmt.Fprintf(w, "faulted (%d):\n", len(steps))
	for i := range steps {
		s := &steps[i]
		fmt.Fprintf(w, "  [%d] %s — %s", i+1, firstNonEmpty(s.ActionName, s.Name, "(step)"), orDash(s.Status))
		if s.IntegrationInstanceName != "" {
			fmt.Fprintf(w, " (%s)", s.IntegrationInstanceName)
		}
		fmt.Fprintln(w)
		if msg := strings.TrimSpace(s.Message); msg != "" {
			if showErrors {
				fmt.Fprintf(w, "      error: %s\n", msg)
			} else {
				fmt.Fprintf(w, "      error: %s\n", truncate(strings.Join(strings.Fields(msg), " "), 300))
			}
		}
		if s.LogsExplorerURL != "" {
			fmt.Fprintf(w, "      logs:  %s\n", s.LogsExplorerURL)
		}
	}
	if !showErrors {
		fmt.Fprintln(w, "  (use --show-errors for full messages)")
	}
}

func printActionResultsSummary(w io.Writer, raw json.RawMessage) {
	records := rawRecordList(raw)
	if len(records) == 0 {
		printGenericItemsSummary(w, "action results", raw)
		return
	}
	statuses := map[string]int{}
	pythonIDs := 0
	for _, record := range records {
		m, ok := rawJSONObject(record)
		if !ok {
			continue
		}
		status := rawScalarString(m["status"])
		if status == "" {
			status = "(unknown)"
		}
		statuses[status]++
		if hasJSONValue(m, "pythonExecutionId") {
			pythonIDs++
		}
	}
	fmt.Fprintf(w, "action_results: %d\n", len(records))
	if len(statuses) > 0 {
		fmt.Fprintln(w, "status:")
		for _, status := range sortedMapKeys(statuses) {
			fmt.Fprintf(w, "- %s: %d\n", status, statuses[status])
		}
	}
	fmt.Fprintf(w, "python_execution_ids: %d present\n", pythonIDs)
}

func printSingleActionResultSummary(w io.Writer, raw json.RawMessage) {
	m, ok := rawJSONObject(raw)
	if !ok {
		printGenericItemsSummary(w, "action result records", raw)
		return
	}
	fmt.Fprintln(w, "action_result:")
	printJSONField(w, m, "status", "status")
	printJSONField(w, m, "resultCode", "result_code")
	for _, field := range []string{
		"message",
		"resultJsonObject",
		"targetedEntitiesJsonObject",
		"resultEntitiesJsonObject",
		"resultValue",
		"scriptResultEntityData",
		"pythonExecutionId",
	} {
		fmt.Fprintf(w, "%s: %t\n", snakeCase(field), hasJSONValue(m, field))
	}
}

func printGenericItemsSummary(w io.Writer, label string, raw json.RawMessage) {
	records := rawRecordList(raw)
	if len(records) > 0 {
		fmt.Fprintf(w, "%s: %d\n", label, len(records))
		return
	}
	switch firstJSONByte(raw) {
	case '[':
		fmt.Fprintf(w, "%s: 0\n", label)
	case '{':
		fmt.Fprintln(w, "response: object")
	default:
		fmt.Fprintln(w, "response: received")
	}
}

func printPendingStepCount(w io.Writer, raw json.RawMessage) {
	switch firstJSONByte(raw) {
	case '{', '[':
	default:
		if count := rawScalarString(raw); count != "" {
			fmt.Fprintf(w, "pending_steps: %s\n", count)
			return
		}
	}
	if m, ok := rawJSONObject(raw); ok {
		for _, key := range []string{"count", "pendingSteps", "pendingStepsCount", "totalCount"} {
			if value := rawScalarString(m[key]); value != "" {
				fmt.Fprintf(w, "pending_steps: %s\n", value)
				return
			}
		}
	}
	printGenericItemsSummary(w, "pending steps", raw)
}

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
