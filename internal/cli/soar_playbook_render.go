package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

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

type faultedStep struct {
	ActionName              string `json:"actionName"`
	Name                    string `json:"name"`
	Status                  string `json:"status"`
	Message                 string `json:"message"`
	IntegrationInstanceName string `json:"integrationInstanceName"`
	LogsExplorerURL         string `json:"logsExplorerUrl"`
}

func printWorkflowSummary(w io.Writer, raw json.RawMessage, showErrors, showSteps bool) {
	m, ok := rawJSONObject(raw)
	if !ok {
		printGenericItemsSummary(w, "workflow summary records", raw)
		return
	}
	if _, hasSteps := m["steps"]; hasSteps {
		printWorkflowInstanceSummary(w, m, showErrors, showSteps)
		return
	}
	fmt.Fprintln(w, "workflow_summary:")
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
	for _, key := range []string{"totalPendingActionSteps", "executionTimeInMs"} {
		printJSONField(w, m, key, snakeCase(key))
	}
	printFaultedSteps(w, m["faultedSteps"], showErrors)
	if showSteps {
		printStepTrace(w, m["completedSteps"], showErrors)
	}
}

func printWorkflowInstanceSummary(w io.Writer, m map[string]json.RawMessage, showErrors, showSteps bool) {
	fmt.Fprintln(w, "workflow_instance:")
	printJSONField(w, m, "instanceId", "instance_id")
	printJSONField(w, m, "status", "status")
	printJSONField(w, m, "caseId", "case_id")

	var steps []playbookTraceStep
	if err := json.Unmarshal(m["steps"], &steps); err != nil || len(steps) == 0 {
		fmt.Fprintln(w, "  steps: 0")
		return
	}

	counts := map[string]int{}
	var faulted []faultedStep
	for i := range steps {
		s := &steps[i]
		st := defaultString(s.Status, "UNKNOWN")
		counts[st]++
		if st == "FAILED" || st == "FAULTED" {
			faulted = append(faulted, faultedStep{
				ActionName:              s.ActionName,
				Name:                    s.Name,
				Status:                  s.Status,
				Message:                 s.Message,
				IntegrationInstanceName: s.IntegrationInstanceName,
				LogsExplorerURL:         s.LogsExplorerURL,
			})
		}
	}
	fmt.Fprintf(w, "  steps: %d\n", len(steps))
	for _, st := range sortedMapKeys(counts) {
		fmt.Fprintf(w, "    %s: %d\n", strings.ToLower(st), counts[st])
	}
	if len(faulted) > 0 {
		raw, _ := json.Marshal(faulted)
		printFaultedSteps(w, raw, showErrors)
	}
	if showSteps {
		raw, _ := json.Marshal(steps)
		printStepTrace(w, raw, showErrors)
	}
}

type playbookTraceStep struct {
	Status                  string `json:"status"`
	Name                    string `json:"name"`
	Integration             string `json:"integration"`
	ActionName              string `json:"actionName"`
	Message                 string `json:"message"`
	IntegrationInstanceName string `json:"integrationInstanceName"`
	LogsExplorerURL         string `json:"logsExplorerUrl"`
	CreationTimeMs          int64  `json:"creationTimeUnixTimeInMs"`
}

func printStepTrace(w io.Writer, rawSteps json.RawMessage, showErrors bool) {
	var steps []playbookTraceStep
	if json.Unmarshal(rawSteps, &steps) != nil || len(steps) == 0 {
		return
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].CreationTimeMs < steps[j].CreationTimeMs })
	fmt.Fprintln(w, "  steps (execution trace, oldest first):")
	for i := range steps {
		s := &steps[i]
		action := strings.TrimSpace(s.Integration + " / " + s.ActionName)
		label := defaultString(strings.TrimSpace(s.Name), action)
		fmt.Fprintf(w, "    %-10s %s\n", defaultString(s.Status, "?"), label)
		if msg := strings.TrimSpace(s.Message); msg != "" {
			if !showErrors {
				msg = truncate(msg, 100)
			}
			fmt.Fprintf(w, "               %s\n", msg)
		}
		if s.LogsExplorerURL != "" {
			fmt.Fprintf(w, "               logs: %s\n", s.LogsExplorerURL)
		}
	}
}

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
