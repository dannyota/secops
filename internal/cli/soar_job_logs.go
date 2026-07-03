package cli

// soar_job_logs.go — job instance run history (v1alpha execution logs).

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// newSOARJobInstanceHistoryCmd shows the execution history of a scheduled job
// instance. These are per-instance run logs (start/end/status/message), not to
// be confused with the CloudLogging-based `soar jobs logs` command.
func newSOARJobInstanceHistoryCmd() *cobra.Command {
	var (
		selector  string
		pageSize  int
		pageToken string
		status    string
	)
	cmd := &cobra.Command{
		Use:   "history --instance <name|id|uniqueIdentifier>",
		Short: "Show run history for a job instance",
		Long: "Run history of a scheduled job instance (start/end/status/message).\n" +
			"For CloudLogging-based logs, use `soar jobs logs`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			instances, err := sc.ListAllJobInstances(ctx)
			if err != nil {
				return err
			}
			ji, err := findModernJobInstance(instances, selector)
			if err != nil {
				return err
			}
			integration, jobID, instanceID, ok := parseJobInstanceName(ji.Name)
			if !ok {
				return fmt.Errorf("cannot parse resource name %q", ji.Name)
			}

			logs, nextToken, total, err := sc.ListJobInstanceLogs(ctx, integration, jobID, instanceID, pageSize, pageToken)
			if err != nil {
				return err
			}

			// Client-side status filter.
			if status != "" {
				want := strings.ToUpper(status)
				filtered := make([]soar.JobInstanceLog, 0, len(logs))
				for _, l := range logs {
					if strings.EqualFold(l.Status, want) {
						filtered = append(filtered, l)
					}
				}
				logs = filtered
			}

			if jsonOut {
				return emitJSON(struct {
					Logs          []soar.JobInstanceLog `json:"logs"`
					NextPageToken string                `json:"next_page_token,omitempty"`
					TotalSize     int                   `json:"total_size"`
				}{Logs: logs, NextPageToken: nextToken, TotalSize: total})
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Run history for %s (%d total)\n\n", ji.DisplayName, total)
			printJobInstanceLogs(w, logs)
			if nextToken != "" {
				fmt.Fprintf(w, "\nNext page: --page-token %s\n", nextToken)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "instance", "", "job instance displayName, id, uniqueIdentifier, or resource name (required)")
	f.IntVar(&pageSize, "page-size", 20, "max log entries per page")
	f.StringVar(&pageToken, "page-token", "", "page token from a previous response")
	f.StringVar(&status, "status", "", "filter by status: SUCCESS or ERROR")
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
}

// printJobInstanceLogs writes a human-readable table of execution logs.
func printJobInstanceLogs(w io.Writer, logs []soar.JobInstanceLog) {
	fmt.Fprintln(w, "START\tEND\tSTATUS\tMESSAGE")
	for _, l := range logs {
		msg := truncateMessage(l.Message, 80)
		start := epochNumberToString(l.StartTime)
		end := epochNumberToString(l.EndTime)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", start, end, l.Status, msg)
	}
	fmt.Fprintf(w, "\n%d log(s)\n", len(logs))
}

// epochNumberToString formats a json.Number epoch-millis timestamp for display.
func epochNumberToString(n json.Number) string {
	v, err := n.Int64()
	if err != nil || v <= 0 {
		s := n.String()
		if s == "" || s == "0" {
			return "-"
		}
		return s
	}
	return msToUTC(v)
}

// truncateMessage shortens a log message to maxLen, appending "..." if cut.
func truncateMessage(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
