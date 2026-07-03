package cli

// soar_job_decode.go — summarize/find/decode/print/emit helpers for SOAR job commands.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"danny.vn/secops/soar"
)

func summarizeSOARJobs(raw json.RawMessage, grep string) ([]soarJobRow, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, err
	}
	rows := make([]soarJobRow, 0, len(records))
	for _, record := range records {
		row, ok := soarJobRowFromRaw(record)
		if !ok {
			continue
		}
		if matchesAny(grep, row.ID, row.UniqueIdentifier, row.Name, row.Integration, row.DefinitionName, row.LastRunStatus) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows, nil
}

func summarizeSOARJobInstances(raw json.RawMessage, grep string) ([]soarJobInstanceRow, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, err
	}
	rows := make([]soarJobInstanceRow, 0, len(records))
	for _, record := range records {
		row, ok := soarJobInstanceRowFromRaw(record)
		if !ok {
			continue
		}
		if matchesAny(grep, row.ID, row.UniqueIdentifier, row.Name, row.Category, row.Integration, row.DefinitionName, row.LastRunStatus) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows, nil
}

func summarizeSOARJobTemplates(raw json.RawMessage, grep string) ([]soarJobTemplateRow, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, err
	}
	rows := make([]soarJobTemplateRow, 0, len(records))
	for _, record := range records {
		row, ok := soarJobTemplateRowFromRaw(record)
		if !ok {
			continue
		}
		if matchesAny(grep, row.ID, row.UniqueIdentifier, row.Name, row.Integration, row.DefinitionName) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows, nil
}

func findSOARJob(raw json.RawMessage, selector string) (json.RawMessage, soarJobRow, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, soarJobRow{}, err
	}
	var matches []struct {
		raw json.RawMessage
		row soarJobRow
	}
	for _, record := range records {
		row, ok := soarJobRowFromRaw(record)
		if !ok {
			continue
		}
		if jobRowMatches(row, selector) {
			matches = append(matches, struct {
				raw json.RawMessage
				row soarJobRow
			}{raw: record, row: row})
		}
	}
	if len(matches) == 0 {
		return nil, soarJobRow{}, fmt.Errorf("no job matches %q (try `soar job list`)", selector)
	}
	if len(matches) > 1 {
		return nil, soarJobRow{}, fmt.Errorf("%q matches %d jobs; use a unique id or uniqueIdentifier", selector, len(matches))
	}
	return matches[0].raw, matches[0].row, nil
}

func findSOARJobInstance(raw json.RawMessage, selector string) (json.RawMessage, soarJobInstanceRow, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, soarJobInstanceRow{}, err
	}
	var matches []struct {
		raw json.RawMessage
		row soarJobInstanceRow
	}
	for _, record := range records {
		row, ok := soarJobInstanceRowFromRaw(record)
		if !ok {
			continue
		}
		if jobInstanceRowMatches(row, selector) {
			matches = append(matches, struct {
				raw json.RawMessage
				row soarJobInstanceRow
			}{raw: record, row: row})
		}
	}
	if len(matches) == 0 {
		return nil, soarJobInstanceRow{}, fmt.Errorf("no job instance matches %q (try `soar job instance list`)", selector)
	}
	if len(matches) > 1 {
		return nil, soarJobInstanceRow{}, fmt.Errorf("%q matches %d job instances; use a unique id or uniqueIdentifier", selector, len(matches))
	}
	return matches[0].raw, matches[0].row, nil
}

func soarJobRowFromRaw(raw json.RawMessage) (soarJobRow, bool) {
	m, ok := rawJSONObject(raw)
	if !ok {
		return soarJobRow{}, false
	}
	enabled, hasEnabled := rawBoolField(m, "isEnabled")
	row := soarJobRow{
		ID:               rawScalarString(m["id"]),
		UniqueIdentifier: rawScalarString(m["uniqueIdentifier"]),
		Name:             rawScalarString(m["name"]),
		Integration:      rawScalarString(m["integration"]),
		DefinitionName:   rawScalarString(m["jobDefinitionName"]),
		LastRunStatus:    rawScalarString(m["lastRunStatus"]),
		LastRunTime:      rawScalarString(m["lastRunTime"]),
		ParameterCount:   jsonArrayLen(m["parameters"]),
	}
	if hasEnabled {
		row.Enabled = &enabled
	}
	return row, row.ID != "" || row.UniqueIdentifier != "" || row.Name != ""
}

func soarJobTemplateRowFromRaw(raw json.RawMessage) (soarJobTemplateRow, bool) {
	m, ok := rawJSONObject(raw)
	if !ok {
		return soarJobTemplateRow{}, false
	}
	enabled, hasEnabled := rawBoolField(m, "isEnabled")
	custom, hasCustom := rawBoolField(m, "isCustom")
	system, hasSystem := rawBoolField(m, "isSystemJob")
	row := soarJobTemplateRow{
		ID:                   rawScalarString(m["id"]),
		UniqueIdentifier:     rawScalarString(m["uniqueIdentifier"]),
		Name:                 rawScalarString(m["name"]),
		Integration:          rawScalarString(m["integration"]),
		DefinitionName:       rawScalarString(m["jobDefinitionName"]),
		RunIntervalInSeconds: rawScalarString(m["runIntervalInSeconds"]),
		ParameterCount:       jsonArrayLen(m["parameters"]),
	}
	if hasEnabled {
		row.Enabled = &enabled
	}
	if hasCustom {
		row.Custom = &custom
	}
	if hasSystem {
		row.SystemJob = &system
	}
	return row, row.ID != "" || row.UniqueIdentifier != "" || row.Name != ""
}

func soarJobInstanceRowFromRaw(raw json.RawMessage) (soarJobInstanceRow, bool) {
	m, ok := rawJSONObject(raw)
	if !ok {
		return soarJobInstanceRow{}, false
	}
	// Accept both legacy keys (isEnabled/isCustom) and modern keys (enabled/custom).
	enabled, hasEnabled := rawBoolField(m, "isEnabled")
	if !hasEnabled {
		enabled, hasEnabled = rawBoolField(m, "enabled")
	}
	custom, hasCustom := rawBoolField(m, "isCustom")
	if !hasCustom {
		custom, hasCustom = rawBoolField(m, "custom")
	}
	row := soarJobInstanceRow{
		ID:               rawScalarString(m["id"]),
		UniqueIdentifier: rawScalarString(m["uniqueIdentifier"]),
		Name:             firstNonEmpty(rawScalarString(m["displayName"]), rawScalarString(m["name"])),
		Category:         rawScalarString(m["category"]),
		Integration:      firstNonEmpty(rawScalarString(m["integration"]), integrationFromResourceName(rawScalarString(m["name"]))),
		DefinitionName:   firstNonEmpty(rawScalarString(m["job"]), rawScalarString(m["jobDefinitionName"])),
		LastRunStatus:    rawScalarString(m["lastRunStatus"]),
		LastRunTime:      rawScalarString(m["lastRunTime"]),
		IntervalSeconds:  rawScalarString(m["intervalSeconds"]),
		ParameterCount:   jsonArrayLen(m["parameters"]),
	}
	if hasEnabled {
		row.Enabled = &enabled
	}
	if hasCustom {
		row.Custom = &custom
	}
	return row, row.ID != "" || row.UniqueIdentifier != "" || row.Name != ""
}

// soarJobInstanceRowFromModern converts a typed modern JobInstance to a row.
func soarJobInstanceRowFromModern(ji soar.JobInstance) soarJobInstanceRow {
	enabled := ji.Enabled
	custom := ji.Custom
	var intervalStr string
	if ji.IntervalSeconds > 0 {
		intervalStr = fmt.Sprintf("%d", ji.IntervalSeconds)
	}
	return soarJobInstanceRow{
		ID:               ji.ID.String(),
		UniqueIdentifier: ji.UniqueIdentifier,
		Name:             ji.DisplayName,
		Integration:      firstNonEmpty(ji.Integration, integrationFromResourceName(ji.Name)),
		DefinitionName:   ji.Job,
		Enabled:          &enabled,
		Custom:           &custom,
		LastRunStatus:    ji.LastRunStatus,
		LastRunTime:      epochNumberToString(ji.LastRunTime),
		IntervalSeconds:  intervalStr,
		ParameterCount:   len(ji.Parameters),
	}
}

// integrationFromResourceName extracts the integration identifier from a
// resource name like "projects/.../integrations/{id}/jobs/{j}/jobInstances/{i}".
func integrationFromResourceName(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == "integrations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// firstNonEmpty is defined in query.go.

func jobRowMatches(row soarJobRow, selector string) bool {
	selector = strings.TrimSpace(selector)
	return selector != "" && (row.ID == selector || row.UniqueIdentifier == selector || row.Name == selector || row.DefinitionName == selector)
}

func jobInstanceRowMatches(row soarJobInstanceRow, selector string) bool {
	selector = strings.TrimSpace(selector)
	return selector != "" && (row.ID == selector || row.UniqueIdentifier == selector ||
		row.Name == selector || row.DefinitionName == selector || row.Integration == selector)
}

func printSOARJobRows(w io.Writer, rows []soarJobRow) {
	fmt.Fprintln(w, "ENABLED\tID\tUNIQUE_IDENTIFIER\tNAME\tINTEGRATION\tLAST_RUN_STATUS\tPARAMS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			boolPtrString(row.Enabled), row.ID, row.UniqueIdentifier, row.Name, row.Integration, defaultString(row.LastRunStatus, "-"), row.ParameterCount)
	}
	fmt.Fprintf(w, "\n%d job(s)\n", len(rows))
}

func printSOARJobInstanceRows(w io.Writer, rows []soarJobInstanceRow) {
	fmt.Fprintln(w, "ENABLED\tID\tNAME\tINTEGRATION\tLAST_RUN_STATUS\tLAST_RUN\tINTERVAL_S\tPARAMS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			boolPtrString(row.Enabled), row.ID, row.Name,
			defaultString(row.Integration, "-"),
			defaultString(row.LastRunStatus, "-"),
			defaultString(row.LastRunTime, "-"),
			defaultString(row.IntervalSeconds, "-"),
			row.ParameterCount)
	}
	fmt.Fprintf(w, "\n%d job instance(s)\n", len(rows))
}

func printSOARJobTemplateRows(w io.Writer, rows []soarJobTemplateRow) {
	fmt.Fprintln(w, "ENABLED\tCUSTOM\tSYSTEM\tID\tUNIQUE_IDENTIFIER\tNAME\tINTEGRATION\tRUN_INTERVAL_SECONDS\tPARAMS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			boolPtrString(row.Enabled),
			boolPtrString(row.Custom),
			boolPtrString(row.SystemJob),
			row.ID,
			row.UniqueIdentifier,
			row.Name,
			row.Integration,
			defaultString(row.RunIntervalInSeconds, "-"),
			row.ParameterCount)
	}
	fmt.Fprintf(w, "\n%d job template(s)\n", len(rows))
}

func emitSOARJobMutationPreview(action string, row soarJobRow, dryRun, assumeYes bool) error {
	if jsonOut {
		return nil
	}
	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SOAR job action against a PRODUCTION tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "Job: %s\n", jobSelectorLabel(row))
	fmt.Fprintf(w, "Enabled: %s\n", boolPtrString(row.Enabled))
	if dryRun {
		fmt.Fprintln(w, "\nDRY RUN — no mutation sent (the target was resolved with a live read). Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "\nRefusing to %s without confirmation (pass --yes). Aborted.\n", strings.ToLower(strings.SplitN(action, " ", 2)[0]))
	}
	return nil
}

func emitSOARJobInstanceMutationPreview(action string, row soarJobInstanceRow, dryRun, assumeYes bool) error {
	if jsonOut {
		return nil
	}
	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SOAR job-instance action against a PRODUCTION tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "Job instance: %s\n", jobInstanceSelectorLabelFull(row))
	fmt.Fprintf(w, "Enabled: %s\n", boolPtrString(row.Enabled))
	if row.Integration != "" {
		fmt.Fprintf(w, "Integration: %s\n", row.Integration)
	}
	if row.Custom != nil {
		fmt.Fprintf(w, "Custom: %s\n", boolPtrString(row.Custom))
	}
	if dryRun {
		fmt.Fprintln(w, "\nDRY RUN — no mutation sent (the target was resolved with a live read). Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "\nRefusing to %s without confirmation (pass --yes). Aborted.\n", strings.ToLower(strings.SplitN(action, " ", 2)[0]))
	}
	return nil
}

func emitSOARJobMutationJSON(action string, row soarJobRow, dryRun, applied bool, response json.RawMessage) error {
	if !jsonOut {
		return nil
	}
	return emitJSON(struct {
		Action   string          `json:"action"`
		Job      soarJobRow      `json:"job"`
		DryRun   bool            `json:"dry_run"`
		Applied  bool            `json:"applied"`
		OK       bool            `json:"ok"`
		Response json.RawMessage `json:"response,omitempty"`
	}{Action: action, Job: row, DryRun: dryRun, Applied: applied, OK: true, Response: response})
}

func emitSOARJobInstanceMutationJSON(action string, row soarJobInstanceRow, dryRun, applied bool, response json.RawMessage) error {
	if !jsonOut {
		return nil
	}
	return emitJSON(struct {
		Action      string             `json:"action"`
		JobInstance soarJobInstanceRow `json:"job_instance"`
		DryRun      bool               `json:"dry_run"`
		Applied     bool               `json:"applied"`
		OK          bool               `json:"ok"`
		Response    json.RawMessage    `json:"response,omitempty"`
	}{Action: action, JobInstance: row, DryRun: dryRun, Applied: applied, OK: true, Response: response})
}

func jobSelectorLabel(row soarJobRow) string {
	for _, value := range []string{row.Name, row.UniqueIdentifier, row.ID, row.DefinitionName} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "(unknown job)"
}

func jobInstanceSelectorLabel(row soarJobInstanceRow) string {
	for _, value := range []string{row.Name, row.UniqueIdentifier, row.ID} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "(unknown job instance)"
}

// jobInstanceSelectorLabelFull returns a detailed label for mutation previews,
// including integration info when available.
func jobInstanceSelectorLabelFull(row soarJobInstanceRow) string {
	label := jobInstanceSelectorLabel(row)
	if row.Integration != "" {
		return fmt.Sprintf("%s [integration=%s]", label, row.Integration)
	}
	return label
}

// parseJobInstanceName parses a v1alpha resource name of the form
// "projects/.../integrations/{i}/jobs/{j}/jobInstances/{inst}" into its
// integration, jobID, and instanceID segments.
func parseJobInstanceName(name string) (string, string, string, bool) {
	parts := strings.Split(name, "/")
	m := make(map[string]string)
	for i := 0; i+1 < len(parts); i += 2 {
		m[parts[i]] = parts[i+1]
	}
	integration := m["integrations"]
	jobID := m["jobs"]
	instanceID := m["jobInstances"]
	return integration, jobID, instanceID, integration != "" && jobID != "" && instanceID != ""
}

// findModernJobInstance searches the modern ListAllJobInstances result for a
// match by displayName, id, or uniqueIdentifier.
func findModernJobInstance(instances []soar.JobInstance, selector string) (*soar.JobInstance, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("empty instance selector")
	}
	var matches []*soar.JobInstance
	for i := range instances {
		ji := &instances[i]
		if ji.DisplayName == selector || ji.ID.String() == selector ||
			ji.UniqueIdentifier == selector || ji.Name == selector {
			matches = append(matches, ji)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no job instance matches %q (try `soar jobs instance list`)", selector)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%q matches %d job instances; use a unique id or uniqueIdentifier", selector, len(matches))
	}
	return matches[0], nil
}

func rawBoolField(m map[string]json.RawMessage, key string) (bool, bool) {
	raw, ok := m[key]
	if !ok {
		return false, false
	}
	var out bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, false
	}
	return out, true
}
