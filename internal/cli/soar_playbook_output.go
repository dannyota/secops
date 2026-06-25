package cli

// soar_playbook_output.go — summary/results/result/python-logs command constructors and JSON print/scan helpers.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

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
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			// Resolve the friendly inputs to the opaque selectors the API needs:
			// a playbook NAME → its definition id, and (when --alert is omitted) the
			// case's single alert identifier.
			if strings.TrimSpace(definition) == "" && strings.TrimSpace(playbook) != "" {
				if definition, err = resolvePlaybookDefinition(ctx, lc, playbook); err != nil {
					return err
				}
			}
			if strings.TrimSpace(alert) == "" {
				if alert, err = resolveCaseAlert(ctx, lc, caseID); err != nil {
					return err
				}
			}
			if strings.TrimSpace(definition) == "" {
				return fmt.Errorf("select the run: pass --playbook <name> or --definition <id>")
			}
			body, err := workflowSummaryBody(caseID, alert, definition, fetchSteps, collapseBlocks)
			if err != nil {
				return err
			}
			render := func(raw json.RawMessage) error {
				if jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				printWorkflowSummary(cmd.OutOrStdout(), raw, showErrors)
				return nil
			}
			return preferModern("soar playbook summary",
				func() error {
					mc, merr := newSOARClient()
					if merr != nil {
						return merr
					}
					// The v1alpha path expects caseId as a string; build the
					// modern body from the same map with the type overridden.
					modernBody := maps.Clone(body)
					modernBody["caseId"] = strconv.Itoa(caseID)
					raw, merr := mc.GetWorkflowInstanceSummary(ctx, modernBody)
					if merr != nil {
						return merr
					}
					return render(raw)
				},
				func() error {
					raw, lerr := lc.GetWorkflowInstanceSummary(ctx, body)
					if lerr != nil {
						return lerr
					}
					return render(raw)
				},
			)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&playbook, "playbook", "", "playbook name — resolved to its definition id via 'soar playbook list'")
	f.StringVar(&alert, "alert", "", "alert identifier (auto-resolved from the case when omitted)")
	f.StringVar(&definition, "definition", "", "playbook definition id (overrides --playbook)")
	f.BoolVar(&fetchSteps, "fetch-steps", true, "include step details")
	f.BoolVar(&collapseBlocks, "collapse-blocks", false, "collapse nested block details")
	f.BoolVar(&showErrors, "show-errors", false, "print full faulted-step error messages (default truncates)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
}

// wrapCloudLogging500 intercepts a legacy SOAR 500 from the Cloud Logging proxy
// (/logging/python) and returns a legible error with the correlation id and a
// pointer to the working triage alternative, instead of dumping the raw payload.
func wrapCloudLogging500(err error) error {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		return err
	}
	cid := ""
	var body struct {
		CorrelationID string `json:"correlationId"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) == nil && body.CorrelationID != "" {
		cid = body.CorrelationID
	}
	hint := "python execution logs are served from Cloud Logging and can 500 " +
		"on some instances (a backend/access condition, not a request-shape bug).\n" +
		"  Triage alternative: secopsctl soar playbook summary --case-id N --playbook \"<name>\"\n" +
		"  (surfaces each faulted step's error + a per-step Logs Explorer deep-link)"
	if cid != "" {
		return fmt.Errorf("%s\n  correlation id (for SecOps support): %s", hint, cid)
	}
	return fmt.Errorf("%s", hint)
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
		Short: "Read Python execution logs from SecOps (can 500; see `playbook summary` for triage)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.CloudLoggingGetPythonLogs(baseContext(), pythonLogsBody(filter, pageToken, sortOrder, pageSize))
			if err != nil {
				return wrapCloudLogging500(err)
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
	return markJSON(cmd)
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
	hasMsg := false
	for i := range steps {
		s := &steps[i]
		fmt.Fprintf(w, "  [%d] %s — %s", i+1, firstNonEmpty(s.ActionName, s.Name, "(step)"), orDash(s.Status))
		if s.IntegrationInstanceName != "" {
			fmt.Fprintf(w, " (%s)", s.IntegrationInstanceName)
		}
		fmt.Fprintln(w)
		if msg := strings.TrimSpace(s.Message); msg != "" {
			hasMsg = true
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
	if hasMsg && !showErrors {
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
