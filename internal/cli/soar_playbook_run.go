package cli

// soar_playbook_run.go — run/debug/test-case/rerun/pending command constructors.

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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
	return markJSON(cmd)
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
				return wrapPlaybookRunError(err)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
}

func newSOARPlaybookRerunCmd() *cobra.Command {
	var (
		caseID      int
		name        string
		identifier  string
		alert       string
		alertGroup  string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "rerun --case-id N (--name <playbook> | --identifier <uuid>)",
		Short: "GUARDED: rerun a playbook on an explicit case/alert",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			body, err := playbookRerunBody(lc, caseID, name, identifier, alertGroup, alert)
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
			raw, err := lc.PlaybookXRerun(baseContext(), body)
			if err != nil {
				return wrapPlaybookRunError(err)
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
	f.StringVar(&name, "name", "", "playbook display name (resolved to identifier automatically)")
	f.StringVar(&identifier, "identifier", "", "original workflow definition identifier (uuid)")
	f.StringVar(&alertGroup, "alert-group", "", "optional alert group identifier")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
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
	return markJSON(cmd)
}
