package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type workflowStepInstanceSummary struct {
	File                       string `json:"file,omitempty"`
	CaseID                     int64  `json:"case_id,omitempty"`
	WorkflowInstanceIdentifier int64  `json:"workflow_instance_identifier,omitempty"`
	WorkflowIdentifier         string `json:"workflow_identifier,omitempty"`
	StepInstanceIdentifier     string `json:"step_instance_identifier,omitempty"`
	Identifier                 string `json:"identifier,omitempty"`
	OriginalStepIdentifier     string `json:"original_step_identifier,omitempty"`
	Status                     string `json:"status,omitempty"`
	AllowedToExecute           *bool  `json:"allowed_to_execute,omitempty"`
	ParameterCount             int    `json:"parameter_count"`
	HasMessage                 bool   `json:"has_message"`
	HasResultValue             bool   `json:"has_result_value"`
	HasJSONResult              bool   `json:"has_json_result"`
	HasResultEntities          bool   `json:"has_result_entities"`
}

func newSOARPlaybookStepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "step",
		Short: "Read and guarded-execute playbook steps",
		Long: "Fetch a workflow step instance for review, then execute an explicit\n" +
			"step-instance JSON body through a dry-run-first guarded command.",
	}
	cmd.AddCommand(newSOARPlaybookStepGetCmd(), newSOARPlaybookStepExecuteCmd())
	return cmd
}

func newSOARPlaybookStepGetCmd() *cobra.Command {
	var (
		caseID                  int
		alert                   string
		workflowIdentifier      string
		stepIdentifier          string
		blockStepID             string
		loopIteration           int
		parentWorkflowIteration int
	)
	cmd := &cobra.Command{
		Use:   "get --case-id N --workflow-identifier <uuid> --step-identifier <uuid>",
		Short: "Read one workflow step instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := workflowStepRequestBody(caseID, alert, workflowIdentifier, stepIdentifier, blockStepID, loopIteration, parentWorkflowIteration)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetWorkflowStepInstance(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			summary, err := summarizeWorkflowStepInstance("", raw)
			if err != nil {
				printGenericItemsSummary(cmd.OutOrStdout(), "workflow step records", raw)
				return nil
			}
			printWorkflowStepInstanceSummary(cmd.OutOrStdout(), summary)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&alert, "alert", "", "optional alert identifier")
	f.StringVar(&workflowIdentifier, "workflow-identifier", "", "workflow identifier (required)")
	f.StringVar(&stepIdentifier, "step-identifier", "", "step identifier (required)")
	f.StringVar(&blockStepID, "block-step-id", "", "optional block step identifier")
	f.IntVar(&loopIteration, "loop-iteration", -1, "optional loop iteration")
	f.IntVar(&parentWorkflowIteration, "parent-workflow-loop-iteration", -1, "optional parent workflow loop iteration")
	_ = cmd.MarkFlagRequired("case-id")
	_ = cmd.MarkFlagRequired("workflow-identifier")
	_ = cmd.MarkFlagRequired("step-identifier")
	return cmd
}

func newSOARPlaybookStepExecuteCmd() *cobra.Command {
	var (
		file        string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "execute --file <step-instance.json>",
		Short: "GUARDED: execute one fetched workflow step instance",
		Long: "Execute one workflow step instance using a JSON body fetched from\n" +
			"`soar playbook step get` or SecOps. Dry-run is the default and prints a\n" +
			"sanitized summary, not the raw step body.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, summary, err := readWorkflowStepInstanceFile(file)
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook step execute", dryRun, yes)
			if err := emitWorkflowStepExecutePreview(summary, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitWorkflowStepExecuteJSON(summary, dr, false, nil)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			resp, err := lc.PlaybookXExecuteStep(baseContext(), raw)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitWorkflowStepExecuteJSON(summary, dr, true, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Playbook step execution requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "workflow step instance JSON file (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func workflowStepRequestBody(caseID int, alert, workflowIdentifier, stepIdentifier, blockStepID string, loopIteration, parentWorkflowIteration int) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	workflowIdentifier = strings.TrimSpace(workflowIdentifier)
	if workflowIdentifier == "" {
		return nil, fmt.Errorf("--workflow-identifier is required")
	}
	stepIdentifier = strings.TrimSpace(stepIdentifier)
	if stepIdentifier == "" {
		return nil, fmt.Errorf("--step-identifier is required")
	}
	body := map[string]any{
		"caseId":             caseID,
		"workflowIdentifier": workflowIdentifier,
		"stepIdentifier":     stepIdentifier,
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	if blockStepID = strings.TrimSpace(blockStepID); blockStepID != "" {
		body["blockStepId"] = blockStepID
	}
	if loopIteration >= 0 {
		body["loopIteration"] = loopIteration
	}
	if parentWorkflowIteration >= 0 {
		body["parentWorkflowLoopIteration"] = parentWorkflowIteration
	}
	return body, nil
}

func readWorkflowStepInstanceFile(path string) (json.RawMessage, workflowStepInstanceSummary, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, workflowStepInstanceSummary{}, fmt.Errorf("--file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, workflowStepInstanceSummary{}, err
	}
	summary, err := summarizeWorkflowStepInstance(path, json.RawMessage(raw))
	if err != nil {
		return nil, workflowStepInstanceSummary{}, err
	}
	if summary.CaseID == 0 {
		return nil, workflowStepInstanceSummary{}, fmt.Errorf("%s: caseId is required", path)
	}
	if summary.StepInstanceIdentifier == "" && summary.Identifier == "" && summary.OriginalStepIdentifier == "" {
		return nil, workflowStepInstanceSummary{}, fmt.Errorf("%s: step identifier is required", path)
	}
	return json.RawMessage(bytes.TrimSpace(raw)), summary, nil
}

func summarizeWorkflowStepInstance(file string, raw json.RawMessage) (workflowStepInstanceSummary, error) {
	m, ok := rawJSONObject(raw)
	if !ok {
		return workflowStepInstanceSummary{}, fmt.Errorf("workflow step instance must be a JSON object")
	}
	summary := workflowStepInstanceSummary{
		File:                   file,
		WorkflowIdentifier:     rawScalarString(m["workflowIdentifier"]),
		StepInstanceIdentifier: rawScalarString(m["stepInstanceIdentifier"]),
		Identifier:             rawScalarString(m["identifier"]),
		OriginalStepIdentifier: rawScalarString(m["originalStepIdentifier"]),
		Status:                 rawScalarString(m["status"]),
		ParameterCount:         jsonArrayLen(m["parameters"]),
		HasMessage:             hasJSONValue(m, "message"),
		HasResultValue:         hasJSONValue(m, "resultValue"),
		HasJSONResult:          hasJSONValue(m, "jsonResultObject"),
		HasResultEntities:      hasJSONValue(m, "resultEntities"),
	}
	if n, ok := rawJSONInt64(m["caseId"]); ok {
		summary.CaseID = n
	}
	if n, ok := rawJSONInt64(m["workflowInstanceIdentifier"]); ok {
		summary.WorkflowInstanceIdentifier = n
	}
	if v, ok := rawJSONBool(m["allowedToExecute"]); ok {
		summary.AllowedToExecute = &v
	}
	return summary, nil
}

func printWorkflowStepInstanceSummary(w io.Writer, summary workflowStepInstanceSummary) {
	fmt.Fprintln(w, "workflow_step:")
	printStepSummaryFields(w, summary)
}

func emitWorkflowStepExecutePreview(summary workflowStepInstanceSummary, dryRun, assumeYes bool) error {
	if jsonOut {
		return nil
	}
	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SOAR playbook step action against a PRODUCTION tenant !!")
	fmt.Fprintln(w, "!! Action: EXECUTE SOAR playbook step")
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "Step summary:")
	printStepSummaryFields(w, summary)
	fmt.Fprintln(w)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to act without confirmation (pass --yes). Aborted.")
	}
	return nil
}

func printStepSummaryFields(w io.Writer, summary workflowStepInstanceSummary) {
	if summary.File != "" {
		fmt.Fprintf(w, "file: %s\n", summary.File)
	}
	if summary.CaseID != 0 {
		fmt.Fprintf(w, "case_id: %d\n", summary.CaseID)
	}
	if summary.WorkflowInstanceIdentifier != 0 {
		fmt.Fprintf(w, "workflow_instance_identifier: %d\n", summary.WorkflowInstanceIdentifier)
	}
	if summary.WorkflowIdentifier != "" {
		fmt.Fprintf(w, "workflow_identifier: %s\n", summary.WorkflowIdentifier)
	}
	if summary.StepInstanceIdentifier != "" {
		fmt.Fprintf(w, "step_instance_identifier: %s\n", summary.StepInstanceIdentifier)
	}
	if summary.Identifier != "" {
		fmt.Fprintf(w, "identifier: %s\n", summary.Identifier)
	}
	if summary.OriginalStepIdentifier != "" {
		fmt.Fprintf(w, "original_step_identifier: %s\n", summary.OriginalStepIdentifier)
	}
	if summary.Status != "" {
		fmt.Fprintf(w, "status: %s\n", summary.Status)
	}
	if summary.AllowedToExecute != nil {
		fmt.Fprintf(w, "allowed_to_execute: %t\n", *summary.AllowedToExecute)
	}
	fmt.Fprintf(w, "parameters: %d\n", summary.ParameterCount)
	fmt.Fprintf(w, "message: %t\n", summary.HasMessage)
	fmt.Fprintf(w, "result_value: %t\n", summary.HasResultValue)
	fmt.Fprintf(w, "json_result: %t\n", summary.HasJSONResult)
	fmt.Fprintf(w, "result_entities: %t\n", summary.HasResultEntities)
}

func emitWorkflowStepExecuteJSON(summary workflowStepInstanceSummary, dryRun, applied bool, response json.RawMessage) error {
	if !jsonOut {
		return nil
	}
	return emitJSON(struct {
		Action   string                      `json:"action"`
		Step     workflowStepInstanceSummary `json:"step"`
		DryRun   bool                        `json:"dry_run"`
		Applied  bool                        `json:"applied"`
		OK       bool                        `json:"ok"`
		Response json.RawMessage             `json:"response,omitempty"`
	}{Action: "playbook step execute", Step: summary, DryRun: dryRun, Applied: applied, OK: true, Response: response})
}

func rawJSONInt64(raw json.RawMessage) (int64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err == nil {
		v, err := n.Int64()
		return v, err == nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := json.Number(s).Int64()
		return v, err == nil
	}
	return 0, false
}

func rawJSONBool(raw json.RawMessage) (bool, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}
