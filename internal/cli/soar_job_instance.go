package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// The `soar job instance` sub-tree: list/get/set/delete/run/history over
// configured job instances (dual-tier: modern v1alpha with legacy fallback).

func newSOARJobInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Manage configured job instances: list, get, run, enable, create, delete",
	}
	cmd.AddCommand(newSOARJobInstanceListCmd(), newSOARJobInstanceGetCmd(),
		newSOARJobInstanceRunCmd(), newSOARJobInstanceHistoryCmd(),
		newSOARJobInstanceSetCmd(), newSOARJobInstanceCreateCmd(), newSOARJobInstanceDeleteCmd())
	return cmd
}

// newSOARJobInstanceSetCmd toggles a scheduled job instance's enabled state.
// Modern path uses a sparse PATCH with updateMask; legacy uses whole-body PUT.
func newSOARJobInstanceSetCmd() *cobra.Command {
	var (
		selector        string
		enable, disable bool
		dryRun, yes     bool
	)
	cmd := &cobra.Command{
		Use:   "set --instance <id|uniqueIdentifier|name> (--enable | --disable)",
		Short: "MUTATING (guarded): enable or disable a scheduled job instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if enable == disable {
				return fmt.Errorf("pass exactly one of --enable / --disable")
			}
			return preferModern("soar jobs instance set",
				func() error {
					sc, err := newSOARClient()
					if err != nil {
						return err
					}
					instances, err := sc.ListAllJobInstances(baseContext())
					if err != nil {
						return err
					}
					ji, err := findModernJobInstance(instances, selector)
					if err != nil {
						return err
					}
					row := soarJobInstanceRowFromModern(*ji)
					integration, jobID, instanceID, ok := parseJobInstanceName(ji.Name)
					if !ok {
						return fmt.Errorf("cannot parse resource name %q", ji.Name)
					}
					action := fmt.Sprintf("job instance set %s enabled=%v", jobInstanceSelectorLabel(row), enable)
					dr, ay := soarGuard(action, dryRun, yes)
					if err := emitSOARJobInstanceMutationPreview("UPDATE SOAR job instance", row, dr, ay); err != nil {
						return err
					}
					if dr || !ay {
						return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
					}
					body := map[string]any{"enabled": enable}
					resp, err := sc.UpdateJobInstance(baseContext(), integration, jobID, instanceID, body, "enabled")
					if err != nil {
						return err
					}
					if jsonOut {
						return emitSOARJobInstanceMutationJSON(action, row, dr, true, resp.Raw)
					}
					fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance updated.")
					return nil
				},
				func() error {
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
			)
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

// newSOARJobInstanceCreateCmd is defined in soar_job_create.go.

// newSOARJobInstanceDeleteCmd deletes a scheduled job instance. Modern path
// uses the v1alpha DELETE; legacy uses the by-id delete.
func newSOARJobInstanceDeleteCmd() *cobra.Command {
	var (
		selector    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "delete --instance <id|uniqueIdentifier|name>",
		Short: "MUTATING (guarded): delete a scheduled job instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return preferModern("soar jobs instance delete",
				func() error {
					sc, err := newSOARClient()
					if err != nil {
						return err
					}
					instances, err := sc.ListAllJobInstances(baseContext())
					if err != nil {
						return err
					}
					ji, err := findModernJobInstance(instances, selector)
					if err != nil {
						return err
					}
					row := soarJobInstanceRowFromModern(*ji)
					integration, jobID, instanceID, ok := parseJobInstanceName(ji.Name)
					if !ok {
						return fmt.Errorf("cannot parse resource name %q", ji.Name)
					}
					action := fmt.Sprintf("job instance delete %s", jobInstanceSelectorLabel(row))
					dr, ay := soarGuard(action, dryRun, yes)
					if err := emitSOARJobInstanceMutationPreview("DELETE SOAR job instance", row, dr, ay); err != nil {
						return err
					}
					if dr || !ay {
						return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
					}
					if err := sc.DeleteJobInstance(baseContext(), integration, jobID, instanceID); err != nil {
						return err
					}
					if jsonOut {
						return emitSOARJobInstanceMutationJSON(action, row, dr, true, nil)
					}
					fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance deleted.")
					return nil
				},
				func() error {
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
			)
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
			return preferModern("soar jobs instance list",
				func() error {
					sc, err := newSOARClient()
					if err != nil {
						return err
					}
					instances, err := sc.ListAllJobInstances(baseContext())
					if err != nil {
						return err
					}
					rows := make([]soarJobInstanceRow, 0, len(instances))
					for _, ji := range instances {
						row := soarJobInstanceRowFromModern(ji)
						if matchesAny(grep, row.ID, row.UniqueIdentifier, row.Name, row.Category, row.Integration, row.DefinitionName, row.LastRunStatus) {
							rows = append(rows, row)
						}
					}
					sort.Slice(rows, func(i, j int) bool {
						return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
					})
					if jsonOut {
						return emitJSON(rows)
					}
					printSOARJobInstanceRows(cmd.OutOrStdout(), rows)
					return nil
				},
				func() error {
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
			)
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive filter over id/name/integration/status")
	return markJSON(cmd)
}

func newSOARJobInstanceGetCmd() *cobra.Command {
	var selector string
	cmd := &cobra.Command{
		Use:   "get --instance <name|id|uniqueIdentifier>",
		Short: "Show details of a single job instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := newSOARClient()
			if err != nil {
				return err
			}
			instances, err := sc.ListAllJobInstances(baseContext())
			if err != nil {
				return err
			}
			ji, err := findModernJobInstance(instances, selector)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, ji.Raw)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Name:                  %s\n", ji.Name)
			fmt.Fprintf(w, "DisplayName:           %s\n", ji.DisplayName)
			fmt.Fprintf(w, "Integration:           %s\n", defaultString(ji.Integration, "-"))
			fmt.Fprintf(w, "Job:                   %s\n", defaultString(ji.Job, "-"))
			fmt.Fprintf(w, "Enabled:               %v\n", ji.Enabled)
			fmt.Fprintf(w, "IntervalSeconds:       %d\n", ji.IntervalSeconds)
			fmt.Fprintf(w, "Advanced:              %v\n", ji.Advanced)
			fmt.Fprintf(w, "Custom:                %v\n", ji.Custom)
			fmt.Fprintf(w, "Author:                %s\n", defaultString(ji.Author, "-"))
			fmt.Fprintf(w, "Description:           %s\n", defaultString(ji.Description, "-"))
			fmt.Fprintf(w, "LastRunStatus:          %s\n", defaultString(ji.LastRunStatus, "-"))
			fmt.Fprintf(w, "LastRunTime:            %s\n", epochNumberToString(ji.LastRunTime))
			fmt.Fprintf(w, "CreateTime:            %s\n", epochNumberToString(ji.CreateTime))
			fmt.Fprintf(w, "UpdateTime:            %s\n", epochNumberToString(ji.UpdateTime))
			fmt.Fprintf(w, "UniqueIdentifier:      %s\n", defaultString(ji.UniqueIdentifier, "-"))
			fmt.Fprintf(w, "Agent:                 %s\n", defaultString(ji.Agent, "-"))
			fmt.Fprintf(w, "DocumentationLink:     %s\n", defaultString(ji.DocumentationLink, "-"))
			fmt.Fprintf(w, "NextScheduledRunTime:  %s\n", epochNumberToString(ji.NextScheduledRunTime))
			if len(ji.Parameters) > 0 {
				fmt.Fprintf(w, "\nParameters (%d):\n", len(ji.Parameters))
				fmt.Fprintln(w, "MANDATORY\tTYPE\tDISPLAY_NAME\tVALUE")
				for _, p := range ji.Parameters {
					fmt.Fprintf(w, "%v\t%s\t%s\t%s\n", p.Mandatory, p.Type, p.DisplayName, p.Value)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&selector, "instance", "", "job instance displayName, id, uniqueIdentifier, or resource name (required)")
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
}

func newSOARJobInstanceRunCmd() *cobra.Command {
	var (
		selector    string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "run --instance <id|uniqueIdentifier|name>",
		Short: "MUTATING (guarded): run one SOAR job instance now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return preferModern("soar jobs instance run",
				func() error {
					sc, err := newSOARClient()
					if err != nil {
						return err
					}
					instances, err := sc.ListAllJobInstances(baseContext())
					if err != nil {
						return err
					}
					ji, err := findModernJobInstance(instances, selector)
					if err != nil {
						return err
					}
					row := soarJobInstanceRowFromModern(*ji)
					integration, jobID, instanceID, ok := parseJobInstanceName(ji.Name)
					if !ok {
						return fmt.Errorf("cannot parse resource name %q", ji.Name)
					}
					action := fmt.Sprintf("job instance run %s", jobInstanceSelectorLabel(row))
					dr, ay := soarGuard(action, dryRun, yes)
					if err := emitSOARJobInstanceMutationPreview("RUN SOAR job instance", row, dr, ay); err != nil {
						return err
					}
					if dr || !ay {
						return emitSOARJobInstanceMutationJSON(action, row, dr, false, nil)
					}
					resp, err := sc.RunJobInstanceOnDemand(baseContext(), integration, jobID, instanceID)
					if err != nil {
						return err
					}
					if jsonOut {
						return emitSOARJobInstanceMutationJSON(action, row, dr, true, resp)
					}
					fmt.Fprintln(cmd.OutOrStdout(), "Done. Job instance run requested.")
					return nil
				},
				func() error {
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
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&selector, "instance", "", "job instance id, uniqueIdentifier, or name (required)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("instance")
	return markJSON(cmd)
}
