package cli

// soar_job.go — row types and command constructors for SOAR job commands.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
}

func newSOARJobInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Inspect, guarded-run, and manage configured job instances",
	}
	cmd.AddCommand(newSOARJobInstanceListCmd(), newSOARJobInstanceRunCmd(),
		newSOARJobInstanceSetCmd(), newSOARJobInstanceCreateCmd(), newSOARJobInstanceDeleteCmd())
	return cmd
}

// newSOARJobInstanceSetCmd toggles a scheduled job instance's enabled state —
// the "disable a noisy or broken scheduled job" path. Whole-body update: the
// instance is fetched fresh, isEnabled overlaid byte-preservingly (no
// float64 round-trip), and the body PUT back. NOTE: the swagger's update shape
// (JobDataUpdateRequest) declares jobDefinitionId/jobDefinitionName, which the
// live list records do not carry — whether the server resolves them from
// id/uniqueIdentifier is exactly what the gated same-value write smoke
// (TestLiveJobInstanceSetWriteSmoke) verifies before this is run for real.
func newSOARJobInstanceSetCmd() *cobra.Command {
	var (
		selector        string
		enable, disable bool
		dryRun, yes     bool
	)
	cmd := &cobra.Command{
		Use:   "set --instance <id|uniqueIdentifier|name> (--enable | --disable)",
		Short: "GUARDED: enable or disable a scheduled job instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if enable == disable {
				return fmt.Errorf("pass exactly one of --enable / --disable")
			}
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
			// RawMessage overlay: every field except isEnabled keeps its exact
			// bytes (a map[string]any round-trip would coerce int64 ids through
			// float64).
			var body map[string]json.RawMessage
			if err := json.Unmarshal(instRaw, &body); err != nil {
				return fmt.Errorf("decode job instance: %w", err)
			}
			enabledRaw, _ := json.Marshal(enable)
			body["isEnabled"] = enabledRaw
			action := fmt.Sprintf("job instance set %s enabled=%v", jobInstanceSelectorLabel(row), enable)
			dr, ay := soarGuard(action, dryRun, yes)
			if err := emitSOARJobInstanceMutationPreview("UPDATE SOAR job instance", row, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
			}
			resp, err := lc.UpdateJobInstance(baseContext(), body)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitSOARJobInstanceMutationJSON(action, row, dr, true, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance updated.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "instance", "", "job instance id, uniqueIdentifier, or name (required)")
	f.BoolVar(&enable, "enable", false, "enable the scheduled instance")
	f.BoolVar(&disable, "disable", false, "disable the scheduled instance")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
}

// newSOARJobInstanceCreateCmd creates a scheduled job instance from a JSON
// body (JobDataAddRequest — typically a copied-and-edited record from
// `instance list --json`, or a definition from `job template list`).
func newSOARJobInstanceCreateCmd() *cobra.Command {
	var (
		file        string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create --file <instance.json>",
		Short: "GUARDED: create a scheduled job instance from a JSON body",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			// Validate it is a JSON object, but send (and preview) the exact file
			// bytes — no float64 round-trip.
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(data, &probe); err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			body := json.RawMessage(bytes.TrimSpace(data))
			return caseAction(fmt.Sprintf("create job instance from %s", file), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.CreateJobInstance(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "job instance JSON body (required)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}

// newSOARJobInstanceDeleteCmd deletes a scheduled job instance by id — the
// clean by-id delete the definition-level DeleteJobData lacks.
func newSOARJobInstanceDeleteCmd() *cobra.Command {
	var (
		selector    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "delete --instance <id|uniqueIdentifier|name>",
		Short: "GUARDED: delete a scheduled job instance",
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
			_, row, err := findSOARJobInstance(raw, selector)
			if err != nil {
				return err
			}
			if row.ID == "" {
				return fmt.Errorf("instance %q carries no numeric id; cannot delete by id", selector)
			}
			action := fmt.Sprintf("job instance delete %s", jobInstanceSelectorLabel(row))
			dr, ay := soarGuard(action, dryRun, yes)
			if err := emitSOARJobInstanceMutationPreview("DELETE SOAR job instance", row, dr, ay); err != nil {
				return err
			}
			if dr || !ay {
				return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
			}
			resp, err := lc.DeleteJobInstance(baseContext(), row.ID)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitSOARJobInstanceMutationJSON(action, row, dr, true, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance deleted.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "instance", "", "job instance id, uniqueIdentifier, or name (required)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
}
