package cli

// soar_job.go — row types and command constructors for SOAR job commands.

import (
	"fmt"
	"os"

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
	Integration      string `json:"integration,omitempty"`
	DefinitionName   string `json:"definition_name,omitempty"`
	Enabled          *bool  `json:"enabled,omitempty"`
	Custom           *bool  `json:"custom,omitempty"`
	LastRunStatus    string `json:"last_run_status,omitempty"`
	LastRunTime      string `json:"last_run_time,omitempty"`
	IntervalSeconds  string `json:"interval_seconds,omitempty"`
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
		Use:   "jobs",
		Short: "Manage SOAR jobs: list, run, templates, instances, revisions",
		Long: "Inspect SOAR jobs and run one explicit job or job instance. Runs are\n" +
			"live SecOps executions and are dry-run by default.",
	}
	cmd.AddCommand(
		newSOARJobListCmd(),
		newSOARJobRunCmd(),
		newSOARJobTemplateCmd(),
		newSOARJobInstanceCmd(),
		newSOARJobLogsCmd(),
		newSOARJobRevisionCmd(),
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
		Short: "MUTATING (guarded): run one installed SOAR job now",
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
		Short: "Browse SOAR job templates",
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
