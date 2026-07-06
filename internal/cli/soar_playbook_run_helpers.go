package cli

// soar_playbook_run_helpers.go — request-body builders and mutation-output helpers for playbook run/debug/rerun commands.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"danny.vn/secops/soar/legacy"
)

// playbookRerunBody builds the request for RerunPlaybook. Unlike run/attach,
// rerun requires originalWorkflowDefinitionIdentifier (the immutable original
// playbook uuid, NOT the current-version identifier). When --name is given
// without --identifier, the playbook is fetched to resolve the original uuid.
//
// When --alert-group is omitted, the alertGroupIdentifier is auto-resolved
// from the case's first alert (the opaque value from
// alerts[].additionalProperties.alertGroupIdentifier, NOT the human-readable
// alert.identifier printed by `cases get`).
func playbookRerunBody(lc *legacy.Client, caseID int, name, identifier, alertGroup, alert string) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	name = strings.TrimSpace(name)
	identifier = strings.TrimSpace(identifier)
	if name == "" && identifier == "" {
		return nil, fmt.Errorf("--name or --identifier is required")
	}
	if identifier == "" {
		resolved, err := resolveOriginalPlaybookID(lc, name)
		if err != nil {
			return nil, err
		}
		identifier = resolved
	}
	alertGroup = strings.TrimSpace(alertGroup)
	if alertGroup == "" {
		resolved, err := resolveAlertGroupFromCase(lc, caseID)
		if err != nil {
			return nil, err
		}
		alertGroup = resolved
	}
	body := map[string]any{
		"cyberCaseId":                          caseID,
		"originalWorkflowDefinitionIdentifier": identifier,
		"alertGroupIdentifier":                 alertGroup,
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	return body, nil
}

// resolveOriginalPlaybookID looks up a playbook by display name and returns
// its originalPlaybookIdentifier (the immutable uuid the rerun API requires).
func resolveOriginalPlaybookID(lc *legacy.Client, name string) (string, error) {
	cards, err := lc.ListPlaybooks(baseContext(), nil)
	if err != nil {
		return "", fmt.Errorf("resolve playbook name: %w", err)
	}
	var cardID string
	for _, c := range cards {
		if c.Name == name {
			cardID = c.Identifier
			break
		}
	}
	if cardID == "" {
		return "", fmt.Errorf("playbook %q not found", name)
	}
	pb, err := lc.GetPlaybook(baseContext(), cardID)
	if err != nil {
		return "", fmt.Errorf("resolve playbook identifier: %w", err)
	}
	var def struct {
		OriginalPlaybookIdentifier string `json:"originalPlaybookIdentifier"`
	}
	if err := json.Unmarshal(pb, &def); err != nil {
		return "", fmt.Errorf("decode playbook definition: %w", err)
	}
	if def.OriginalPlaybookIdentifier == "" {
		return cardID, nil
	}
	return def.OriginalPlaybookIdentifier, nil
}

func playbookRunBody(lc *legacy.Client, caseID int, name, identifier, alertGroup, alert string) (map[string]any, error) {
	if caseID <= 0 {
		return nil, fmt.Errorf("--case-id is required")
	}
	name = strings.TrimSpace(name)
	identifier = strings.TrimSpace(identifier)
	if name == "" && identifier == "" {
		return nil, fmt.Errorf("--name or --identifier is required")
	}
	if identifier == "" {
		resolved, err := resolveOriginalPlaybookID(lc, name)
		if err != nil {
			return nil, err
		}
		identifier = resolved
	}
	alertGroup = strings.TrimSpace(alertGroup)
	if alertGroup == "" {
		resolved, err := resolveAlertGroupFromCase(lc, caseID)
		if err != nil {
			return nil, err
		}
		alertGroup = resolved
	}
	body := map[string]any{
		"cyberCaseId":                          caseID,
		"originalWorkflowDefinitionIdentifier": identifier,
		"inputParameters":                      []any{},
		"alertGroupIdentifier":                 alertGroup,
	}
	if name != "" {
		body["wfName"] = name
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertIdentifier"] = alert
	}
	return body, nil
}

// resolveAlertGroupFromCase fetches the case detail and returns the first
// alert's alertGroupIdentifier (the opaque internal value the run/rerun APIs
// require). This is NOT the same as alert.identifier (the human-readable name
// printed by `cases get`).
func resolveAlertGroupFromCase(lc *legacy.Client, caseID int) (string, error) {
	raw, err := lc.GetCaseFullDetails(baseContext(), caseID)
	if err != nil {
		return "", fmt.Errorf("resolve alert group from case %d: %w", caseID, err)
	}
	var detail struct {
		Alerts []struct {
			AdditionalProperties struct {
				AlertGroupIdentifier string `json:"alertGroupIdentifier"`
			} `json:"additionalProperties"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return "", fmt.Errorf("decode case %d alerts: %w", caseID, err)
	}
	for _, a := range detail.Alerts {
		if g := strings.TrimSpace(a.AdditionalProperties.AlertGroupIdentifier); g != "" {
			return g, nil
		}
	}
	return "", fmt.Errorf("case %d has no alerts with an alertGroupIdentifier", caseID)
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
		fmt.Fprintln(w, "DRY RUN — no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to %s without confirmation (pass --yes). Aborted.\n", strings.ToLower(action))
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
