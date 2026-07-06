package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// Playbook-execution error inspector for a SOAR case. Walks every alert's
// playbooks, fetches the workflow instance summary (faultedSteps, completedSteps,
// retryPendingSteps), and recursively descends into nested BLOCK playbooks. The
// output is a tree showing every faulted step with its full error message.
//
// API flow (captured from the live SecOps console):
//   1. GetWorkflowInstancesCards → playbook list per alert
//   2. GetWorkflowInstance (shouldFetchSteps) → step tree + instanceId
//   3. GetWorkflowInstanceSummary (parentWorkflowInstanceId) → faulted/completed
//   4. For BLOCK steps: extract NestedWorkflowIdentifier from parameters →
//      recursive GetWorkflowInstanceSummary with nestedStepIdentifier

// ── typed structs (decode the subset we render) ─────────────────────────────

type wfCardsResponse struct {
	Payload []wfCard `json:"payload"`
}

type wfCard struct {
	StatusRaw            json.RawMessage `json:"status"`
	Name                 string          `json:"name"`
	DefinitionIdentifier string          `json:"definitionIdentifier"`
	RunCount             int             `json:"runCount"`
}

func (c *wfCard) status() string {
	var s string
	if json.Unmarshal(c.StatusRaw, &s) == nil {
		return s
	}
	var n int
	if json.Unmarshal(c.StatusRaw, &n) == nil {
		return wfStatusLabel(strconv.Itoa(n))
	}
	return string(c.StatusRaw)
}

type wfInstance struct {
	InstanceID int            `json:"instanceId"`
	Name       string         `json:"name"`
	StatusRaw  flexibleString `json:"status"`
	Identifier string         `json:"identifier"`
	Steps      []wfStep       `json:"steps"`
}

type wfStep struct {
	Identifier   string         `json:"identifier"`
	InstanceName string         `json:"instanceName"`
	Integration  string         `json:"integration"`
	ActionName   string         `json:"actionName"`
	TypeRaw      flexibleString `json:"type"`
	StatusRaw    flexibleString `json:"status"`
	Parameters   []wfStepParam  `json:"parameters"`
}

// flexibleString decodes a JSON value that may be a string or an integer
// (the legacy API returns integer enums, the modern v1alpha returns strings).
type flexibleString string

func (f *flexibleString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flexibleString(s)
		return nil
	}
	var n int
	if json.Unmarshal(b, &n) == nil {
		*f = flexibleString(strconv.Itoa(n))
		return nil
	}
	*f = flexibleString(strings.Trim(string(b), "\""))
	return nil
}

func (f flexibleString) String() string { return string(f) }

func isBlock(t string) bool { return t == "BLOCK" || t == "5" }
func isNoStatus(s string) bool {
	return s == "NO_STATUS" || s == "0" || s == "-1" || s == ""
}

// wfStatusLabel maps legacy integer status codes to human-readable labels.
// Sourced from ApiActionStatusEnum in the swagger.
func wfStatusLabel(s string) string {
	switch s {
	case "0":
		return "FAULTED"
	case "1":
		return "IN_PROGRESS"
	case "2":
		return "COMPLETED"
	case "3":
		return "PENDING_USER_INPUT"
	case "4":
		return "PENDING_PREVIOUS_STEPS"
	case "5":
		return "STARTED"
	case "6":
		return "FAULTED_AND_SKIPPED"
	case "7":
		return "HANDLED_TIMED_OUT"
	case "8":
		return "UNHANDLED_TIMED_OUT"
	case "9":
		return "TERMINATED"
	case "10":
		return "NOT_RUN_AND_SKIPPED"
	case "11":
		return "PENDING_ACTION_TIMEOUT"
	case "12":
		return "PENDING_ACTION_TIMEOUT_AND_SKIPPED"
	case "13":
		return "PENDING_RETRY"
	case "-1":
		return "NO_STATUS"
	}
	return s
}

func (s *wfStep) nestedWorkflowID() string {
	for _, p := range s.Parameters {
		if p.Name == "NestedWorkflowIdentifier" {
			return p.Value
		}
	}
	return ""
}

type wfStepParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type wfSummary struct {
	CompletedSteps    []wfSummaryStep `json:"completedSteps"`
	FaultedSteps      []wfSummaryStep `json:"faultedSteps"`
	RetryPendingSteps []wfSummaryStep `json:"retryPendingSteps"`
	ExecutionTimeInMs int64           `json:"executionTimeInMs"`
}

type wfSummaryStep struct {
	InstanceName string         `json:"instanceName"`
	Integration  string         `json:"integration"`
	ActionName   string         `json:"actionName"`
	Status       flexibleString `json:"status"`
	Message      string         `json:"message"`
	ResultCode   int            `json:"resultCode"`
}

// ── collected result for --json ─────────────────────────────────────────────

type pbErrorResult struct {
	CaseID int             `json:"caseId"`
	Title  string          `json:"title"`
	Alerts []pbAlertResult `json:"alerts"`
}

type pbAlertResult struct {
	AlertIdentifier string             `json:"alertIdentifier"`
	AlertName       string             `json:"alertName"`
	Playbooks       []pbPlaybookResult `json:"playbooks"`
}

type pbPlaybookResult struct {
	Name     string           `json:"name"`
	Status   string           `json:"status"`
	RunCount int              `json:"runCount"`
	Summary  *wfSummary       `json:"summary,omitempty"`
	Nested   []pbNestedResult `json:"nested,omitempty"`
}

type pbNestedResult struct {
	BlockName string     `json:"blockName"`
	Status    string     `json:"status"`
	Summary   *wfSummary `json:"summary,omitempty"`
}

// ── command ─────────────────────────────────────────────────────────────────

func newCasePlaybookErrorsCmd() *cobra.Command {
	var idFlag int
	var alertFlag string
	cmd := &cobra.Command{
		Use:   "playbook-errors <case-id>",
		Short: "Show playbook execution errors for a case (including nested blocks)",
		Long: "Walk every alert's playbook(s) in a case and show faulted steps with\n" +
			"full error messages. Descends into nested BLOCK playbooks recursively.\n" +
			"Completed/pending step counts are shown as a summary line per playbook.\n\n" +
			"Uses the legacy AppKey API (reliable lane).",
		Example: "  secopsctl soar case playbook-errors 6139\n" +
			"  secopsctl soar case playbook-errors 6139 --json\n" +
			"  secopsctl soar case playbook-errors 6139 --alert <alert-identifier>",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := idFlag
			if len(args) == 1 {
				n, err := strconv.Atoi(strings.TrimSpace(args[0]))
				if err != nil {
					return fmt.Errorf("case id must be an integer, got %q", args[0])
				}
				id = n
			}
			if id == 0 {
				return fmt.Errorf("a case id is required (positional or --id)")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			return runPlaybookErrors(lc, id, alertFlag)
		},
	}
	cmd.Flags().IntVar(&idFlag, "id", 0, "SOAR case id (alternative to the positional arg)")
	cmd.Flags().StringVar(&alertFlag, "alert", "", "scope to one alert (identifier from 'soar case get')")
	return markJSON(cmd)
}

// pbAlert is the alert subset decoded from GetCaseFullDetails for the
// playbook-errors walk. The workflow APIs need alertGroupIdentifier (from
// additionalProperties), NOT the top-level identifier field.
type pbAlert struct {
	Identifier           string `json:"identifier"`
	Name                 string `json:"name"`
	HasWorkflows         bool   `json:"hasWorkflows"`
	AdditionalProperties struct {
		AlertGroupIdentifier string `json:"alertGroupIdentifier"`
	} `json:"additionalProperties"`
}

func (a *pbAlert) workflowAlertID() string {
	if id := a.AdditionalProperties.AlertGroupIdentifier; id != "" {
		return id
	}
	return a.Identifier
}

func runPlaybookErrors(lc *legacy.Client, caseID int, alertFilter string) error {
	ctx := baseContext()

	raw, err := lc.GetCaseFullDetails(ctx, caseID)
	if err != nil {
		return fmt.Errorf("get case: %w", err)
	}
	var cs struct {
		ID     int       `json:"id"`
		Title  string    `json:"title"`
		Alerts []pbAlert `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &cs); err != nil {
		return fmt.Errorf("decode case: %w", err)
	}

	result := pbErrorResult{CaseID: cs.ID, Title: cs.Title}
	w := os.Stdout

	if !jsonOut {
		fmt.Fprintf(w, "Case %d: %s\n", cs.ID, cs.Title)
	}

	for i := range cs.Alerts {
		a := &cs.Alerts[i]
		wfAlertID := a.workflowAlertID()
		if alertFilter != "" && a.Identifier != alertFilter && wfAlertID != alertFilter {
			continue
		}
		if !a.HasWorkflows {
			continue
		}
		ar, err := fetchAlertPlaybookErrors(lc, caseID, wfAlertID, a.Name, w)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: alert %s: %v\n", a.Name, err)
			continue
		}
		result.Alerts = append(result.Alerts, *ar)
	}

	if jsonOut {
		return emitJSON(result)
	}
	if len(result.Alerts) == 0 {
		fmt.Fprintln(w, "\nNo playbooks with execution data found.")
	}
	return nil
}

func fetchAlertPlaybookErrors(lc *legacy.Client, caseID int, alertID, alertName string, w io.Writer) (*pbAlertResult, error) {
	ctx := baseContext()

	cardsRaw, err := lc.GetWorkflowInstancesCards(ctx, map[string]any{
		"caseId":          caseID,
		"alertIdentifier": alertID,
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow cards: %w", err)
	}

	var cards []wfCard
	if err := json.Unmarshal(cardsRaw, &cards); err != nil {
		// The modern v1alpha wraps in {payload:[...]}, legacy returns bare array.
		var wrapped wfCardsResponse
		if err2 := json.Unmarshal(cardsRaw, &wrapped); err2 != nil {
			return nil, fmt.Errorf("decode workflow cards: %w", err)
		}
		cards = wrapped.Payload
	}

	ar := &pbAlertResult{
		AlertIdentifier: alertID,
		AlertName:       alertName,
	}

	for ci := range cards {
		card := &cards[ci]
		pr, err := fetchPlaybookErrors(lc, caseID, alertID, card, w)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: playbook %s: %v\n", card.Name, err)
			continue
		}
		ar.Playbooks = append(ar.Playbooks, *pr)
	}
	return ar, nil
}

func fetchPlaybookErrors(lc *legacy.Client, caseID int, alertID string, card *wfCard, w io.Writer) (*pbPlaybookResult, error) {
	ctx := baseContext()

	// Fetch the full instance to get instanceId and step tree.
	instRaw, err := lc.GetWorkflowInstance(ctx, map[string]any{
		"caseId":                   caseID,
		"alertIdentifier":          alertID,
		"shouldFetchSteps":         true,
		"definitionIdentifier":     card.DefinitionIdentifier,
		"collapseBlocks":           true,
		"loopsRequestedIterations": []any{},
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow instance: %w", err)
	}

	var inst wfInstance
	if err := json.Unmarshal(instRaw, &inst); err != nil {
		return nil, fmt.Errorf("decode workflow instance: %w", err)
	}

	// Fetch the summary (faulted/completed/pending counts + error messages).
	summaryRaw, err := lc.GetWorkflowInstanceSummary(ctx, map[string]any{
		"caseId":                      caseID,
		"alertIdentifier":             alertID,
		"shouldFetchSteps":            true,
		"definitionIdentifier":        inst.Identifier,
		"parentWorkflowInstanceId":    strconv.Itoa(inst.InstanceID),
		"loopsRequestedIterations":    []any{},
		"parentWorkflowLoopIteration": nil,
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow summary: %w", err)
	}

	var summary wfSummary
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		return nil, fmt.Errorf("decode workflow summary: %w", err)
	}

	pr := &pbPlaybookResult{
		Name:     card.Name,
		Status:   card.status(),
		RunCount: card.RunCount,
		Summary:  &summary,
	}

	if !jsonOut {
		fmt.Fprintln(w)
		renderPlaybookSummary(w, card.Name, card.RunCount, card.status(), &summary, "")
	}

	// Recurse into nested BLOCK steps.
	for si := range inst.Steps {
		step := &inst.Steps[si]
		if !isBlock(step.TypeRaw.String()) || isNoStatus(step.StatusRaw.String()) {
			continue
		}
		nestedDefID := step.nestedWorkflowID()
		if nestedDefID == "" {
			continue
		}
		nr, err := fetchNestedErrors(lc, caseID, alertID, inst.InstanceID, step, nestedDefID, w)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: nested block %s: %v\n", step.InstanceName, err)
			continue
		}
		pr.Nested = append(pr.Nested, *nr)
	}
	return pr, nil
}

func fetchNestedErrors(lc *legacy.Client, caseID int, alertID string, parentInstanceID int, step *wfStep, nestedDefID string, w io.Writer) (*pbNestedResult, error) {
	ctx := baseContext()

	summaryRaw, err := lc.GetWorkflowInstanceSummary(ctx, map[string]any{
		"caseId":                      caseID,
		"alertIdentifier":             alertID,
		"shouldFetchSteps":            true,
		"definitionIdentifier":        nestedDefID,
		"parentWorkflowInstanceId":    strconv.Itoa(parentInstanceID),
		"nestedStepIdentifier":        step.Identifier,
		"loopsRequestedIterations":    []any{},
		"parentWorkflowLoopIteration": nil,
	})
	if err != nil {
		return nil, fmt.Errorf("get nested summary: %w", err)
	}

	var summary wfSummary
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		return nil, fmt.Errorf("decode nested summary: %w", err)
	}

	nr := &pbNestedResult{
		BlockName: step.InstanceName,
		Status:    step.StatusRaw.String(),
		Summary:   &summary,
	}

	if !jsonOut {
		renderNestedSummary(w, step.InstanceName, step.StatusRaw.String(), &summary)
	}
	return nr, nil
}

// ── rendering ───────────────────────────────────────────────────────────────

func renderPlaybookSummary(w io.Writer, name string, runCount int, status string, s *wfSummary, prefix string) {
	timeStr := "-"
	if s.ExecutionTimeInMs > 0 {
		timeStr = fmt.Sprintf("%.1fs", float64(s.ExecutionTimeInMs)/1000)
	}
	fmt.Fprintf(w, "%s▶ %s (#%d)  %s  %s\n", prefix, name, runCount, wfStatusLabel(status), timeStr)
	fmt.Fprintf(w, "%s  %d completed · %d faulted · %d pending\n",
		prefix, len(s.CompletedSteps), len(s.FaultedSteps), len(s.RetryPendingSteps))

	renderFaultedSteps(w, s.FaultedSteps, prefix+"  ")
	renderRetrySteps(w, s.RetryPendingSteps, prefix+"  ")
}

func renderNestedSummary(w io.Writer, blockName, status string, s *wfSummary) {
	if len(s.FaultedSteps) == 0 && len(s.RetryPendingSteps) == 0 {
		return
	}
	timeStr := "-"
	if s.ExecutionTimeInMs > 0 {
		timeStr = fmt.Sprintf("%.1fs", float64(s.ExecutionTimeInMs)/1000)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  ├─ %s  %s  %s\n", blockName, wfStatusLabel(status), timeStr)
	fmt.Fprintf(w, "  │  %d completed · %d faulted · %d pending\n",
		len(s.CompletedSteps), len(s.FaultedSteps), len(s.RetryPendingSteps))

	renderFaultedSteps(w, s.FaultedSteps, "  │  ")
	renderRetrySteps(w, s.RetryPendingSteps, "  │  ")
}

func renderFaultedSteps(w io.Writer, steps []wfSummaryStep, prefix string) {
	for i := range steps {
		s := &steps[i]
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s✗ %s  (%s / %s)\n", prefix, s.InstanceName, s.Integration, s.ActionName)
		renderErrorMessage(w, s.Message, prefix+"  ")
	}
}

func renderRetrySteps(w io.Writer, steps []wfSummaryStep, prefix string) {
	for i := range steps {
		s := &steps[i]
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s⟳ %s  (%s / %s)  RETRY_PENDING\n", prefix, s.InstanceName, s.Integration, s.ActionName)
		if s.Message != "" {
			renderErrorMessage(w, s.Message, prefix+"  ")
		}
	}
}

// renderErrorMessage prints a full error message with word-wrap and embedded
// JSON pretty-printing. If the message ends with a JSON object ({...}), it is
// extracted and printed indented on separate lines.
func renderErrorMessage(w io.Writer, msg, prefix string) {
	if msg == "" {
		return
	}
	// Try to detect and split trailing JSON from the human-readable prefix.
	text, jsonStr := splitTrailingJSON(msg)
	if text != "" {
		writeWrapped(w, text, prefix, 100)
	}
	if jsonStr != "" {
		var obj any
		if json.Unmarshal([]byte(jsonStr), &obj) == nil {
			pretty, err := json.MarshalIndent(obj, prefix, "  ")
			if err == nil {
				fmt.Fprintf(w, "%s%s\n", prefix, pretty)
				return
			}
		}
		writeWrapped(w, jsonStr, prefix, 100)
	}
}

// splitTrailingJSON splits a message into (text, json) if it ends with a JSON
// object. Scans backwards for balanced braces. Returns (msg, "") if no valid
// trailing JSON is found.
func splitTrailingJSON(msg string) (string, string) {
	trimmed := strings.TrimSpace(msg)
	if !strings.HasSuffix(trimmed, "}") {
		return msg, ""
	}
	// Walk backwards to find the matching opening brace.
	depth := 0
	inStr := false
	esc := false
	start := -1
	for i := len(trimmed) - 1; i >= 0; i-- {
		c := trimmed[i]
		if esc {
			esc = false
			continue
		}
		if c == '\\' && inStr {
			esc = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == '}' {
			depth++
		} else if c == '{' {
			depth--
			if depth == 0 {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return msg, ""
	}
	candidate := trimmed[start:]
	var obj any
	if json.Unmarshal([]byte(candidate), &obj) != nil {
		return msg, ""
	}
	return strings.TrimSpace(trimmed[:start]), candidate
}

// writeWrapped writes text word-wrapped at width, with each line prefixed.
func writeWrapped(w io.Writer, text, prefix string, width int) {
	maxLine := max(width-len(prefix), 40)
	words := strings.Fields(text)
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if len(line)+1+len(word) > maxLine {
			fmt.Fprintf(w, "%s%s\n", prefix, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
}
