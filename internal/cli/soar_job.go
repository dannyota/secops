package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type soarJobRow struct {
	ID               string `json:"id,omitempty"`
	UniqueIdentifier string `json:"unique_identifier,omitempty"`
	Name             string `json:"name,omitempty"`
	Integration      string `json:"integration,omitempty"`
	DefinitionName   string `json:"definition_name,omitempty"`
	Enabled          *bool  `json:"enabled,omitempty"`
	LastRunStatus    string `json:"last_run_status,omitempty"`
	LastRunTime      string `json:"last_run_time,omitempty"`
	ParameterCount   int    `json:"parameter_count"`
}

type soarJobInstanceRow struct {
	ID               string `json:"id,omitempty"`
	UniqueIdentifier string `json:"unique_identifier,omitempty"`
	Name             string `json:"name,omitempty"`
	Category         string `json:"category,omitempty"`
	Enabled          *bool  `json:"enabled,omitempty"`
	Custom           *bool  `json:"custom,omitempty"`
	ParameterCount   int    `json:"parameter_count"`
}

type soarJobTemplateRow struct {
	ID                   string `json:"id,omitempty"`
	UniqueIdentifier     string `json:"unique_identifier,omitempty"`
	Name                 string `json:"name,omitempty"`
	Integration          string `json:"integration,omitempty"`
	DefinitionName       string `json:"definition_name,omitempty"`
	Enabled              *bool  `json:"enabled,omitempty"`
	Custom               *bool  `json:"custom,omitempty"`
	SystemJob            *bool  `json:"system_job,omitempty"`
	RunIntervalInSeconds string `json:"run_interval_in_seconds,omitempty"`
	ParameterCount       int    `json:"parameter_count"`
}

func newSOARJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Inspect and guarded-run SOAR jobs",
		Long: "Inspect SOAR jobs and run one explicit job or job instance. Runs are\n" +
			"live SecOps executions and are dry-run by default.",
	}
	cmd.AddCommand(
		newSOARJobListCmd(),
		newSOARJobRunCmd(),
		newSOARJobTemplateCmd(),
		newSOARJobInstanceCmd(),
		newSOARJobLogsCmd(),
	)
	return cmd
}

func newSOARJobListCmd() *cobra.Command {
	var grep string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed SOAR jobs and last-run status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListInstalledJobs(baseContext())
			if err != nil {
				return err
			}
			rows, err := summarizeSOARJobs(raw, grep)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(rows)
			}
			printSOARJobRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive filter over id/name/integration/status")
	return cmd
}

func newSOARJobRunCmd() *cobra.Command {
	var (
		selector    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "run --job <id|uniqueIdentifier|name>",
		Short: "GUARDED: run one installed SOAR job now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListInstalledJobs(baseContext())
			if err != nil {
				return err
			}
			jobRaw, row, err := findSOARJob(raw, selector)
			if err != nil {
				return err
			}
			action := fmt.Sprintf("job run %s", jobSelectorLabel(row))
			dr, ay := soarGuard(action, dryRun, yes)
			if err := emitSOARJobMutationPreview("RUN SOAR job", row, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitSOARJobMutationJSON(action, row, dr, false, nil)
			}
			resp, err := lc.RunJob(baseContext(), jobRaw)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitSOARJobMutationJSON(action, row, dr, true, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Job run requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "job", "", "job id, uniqueIdentifier, name, or definition name (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func newSOARJobLogsCmd() *cobra.Command {
	var (
		filter    string
		pageSize  int
		pageToken string
		sortOrder string
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read SOAR Python execution logs for jobs/actions",
		Long: "Read Python execution logs from SecOps Cloud Logging. The endpoint\n" +
			"covers integration actions and jobs; use --filter to narrow the server-side\n" +
			"query, for example labels.job_name=~\"^.\" or labels.action_name=~\"^.\".\n" +
			"Human output prints counts only; --json emits the raw payload.",
		Args: cobra.NoArgs,
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
	return cmd
}

func newSOARJobTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Inspect SOAR job templates",
	}
	cmd.AddCommand(newSOARJobTemplateListCmd())
	return cmd
}

func newSOARJobTemplateListCmd() *cobra.Command {
	var grep string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SOAR job templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListJobTemplates(baseContext())
			if err != nil {
				return err
			}
			rows, err := summarizeSOARJobTemplates(raw, grep)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(rows)
			}
			printSOARJobTemplateRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive filter over id/name/integration")
	return cmd
}

func newSOARJobInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Inspect and guarded-run configured job instances",
	}
	cmd.AddCommand(newSOARJobInstanceListCmd(), newSOARJobInstanceRunCmd())
	return cmd
}

func newSOARJobInstanceListCmd() *cobra.Command {
	var grep string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured SOAR job instances",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListJobInstances(baseContext())
			if err != nil {
				return err
			}
			rows, err := summarizeSOARJobInstances(raw, grep)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(rows)
			}
			printSOARJobInstanceRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive filter over id/name/category")
	return cmd
}

func newSOARJobInstanceRunCmd() *cobra.Command {
	var (
		selector    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "run --instance <id|uniqueIdentifier|name>",
		Short: "GUARDED: run one SOAR job instance now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListJobInstances(baseContext())
			if err != nil {
				return err
			}
			instRaw, row, err := findSOARJobInstance(raw, selector)
			if err != nil {
				return err
			}
			action := fmt.Sprintf("job instance run %s", jobInstanceSelectorLabel(row))
			dr, ay := soarGuard(action, dryRun, yes)
			if err := emitSOARJobInstanceMutationPreview("RUN SOAR job instance", row, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
			}
			resp, err := lc.RunJobInstance(baseContext(), instRaw)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitSOARJobInstanceMutationJSON(action, row, dr, true, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance run requested.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "instance", "", "job instance id, uniqueIdentifier, or name (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("instance")
	return cmd
}

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
		if matchesAny(grep, row.ID, row.UniqueIdentifier, row.Name, row.Category) {
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
	enabled, hasEnabled := rawBoolField(m, "isEnabled")
	custom, hasCustom := rawBoolField(m, "isCustom")
	row := soarJobInstanceRow{
		ID:               rawScalarString(m["id"]),
		UniqueIdentifier: rawScalarString(m["uniqueIdentifier"]),
		Name:             rawScalarString(m["name"]),
		Category:         rawScalarString(m["category"]),
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

func jobRowMatches(row soarJobRow, selector string) bool {
	selector = strings.TrimSpace(selector)
	return selector != "" && (row.ID == selector || row.UniqueIdentifier == selector || row.Name == selector || row.DefinitionName == selector)
}

func jobInstanceRowMatches(row soarJobInstanceRow, selector string) bool {
	selector = strings.TrimSpace(selector)
	return selector != "" && (row.ID == selector || row.UniqueIdentifier == selector || row.Name == selector)
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
	fmt.Fprintln(w, "ENABLED\tID\tUNIQUE_IDENTIFIER\tNAME\tCATEGORY\tPARAMS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
			boolPtrString(row.Enabled), row.ID, row.UniqueIdentifier, row.Name, row.Category, row.ParameterCount)
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
		fmt.Fprintln(w, "\nDRY RUN -- no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "\nRefusing to act without confirmation (pass --yes). Aborted.")
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
	fmt.Fprintf(w, "Job instance: %s\n", jobInstanceSelectorLabel(row))
	fmt.Fprintf(w, "Enabled: %s\n", boolPtrString(row.Enabled))
	if dryRun {
		fmt.Fprintln(w, "\nDRY RUN -- no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "\nRefusing to act without confirmation (pass --yes). Aborted.")
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
