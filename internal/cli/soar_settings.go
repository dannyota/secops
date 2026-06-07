package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/soar/legacy"
)

// newSOARSettingsCmd groups the singleton case-routing policy get/set verbs.
// These are one-record settings (no list/id/delete), so they are imperative rather
// than reconcile surfaces.
func newSOARSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Read/set singleton SOAR case-routing policies",
	}
	cmd.AddCommand(
		newSOARPolicyCmd("case-assignment", "case auto-assignment policy", "assignmentPolicy",
			func(lc *legacy.Client) func(context.Context) (legacy.RawJSON, error) {
				return lc.GetCaseAssignmentPolicySettings
			},
			func(lc *legacy.Client) func(context.Context, any) (legacy.RawJSON, error) {
				return lc.AddOrUpdateCaseAssignmentPolicySettings
			}),
		newSOARPolicyCmd("move-case-policy", "cross-environment case-move policy", "moveCaseBetweenEnvironmentsPolicy",
			func(lc *legacy.Client) func(context.Context) (legacy.RawJSON, error) {
				return lc.GetMoveCaseBetweenEnvironmentsPolicySettings
			},
			func(lc *legacy.Client) func(context.Context, any) (legacy.RawJSON, error) {
				return lc.AddOrUpdateMoveCaseBetweenEnvironmentsPolicySettings
			}),
		newSOARAPIKeysCmd(),
	)
	return cmd
}

// newSOARAPIKeysCmd lists the SOAR external-API keys as metadata — read-only, and
// the secret value is never surfaced (the list endpoint masks it; the SDK drops it
// entirely). Create/revoke are not wired: those endpoints aren't on the external
// API surface and need the console request to confirm (the key value would be
// shown once on create and never persisted — House Rule 4).
func newSOARAPIKeysCmd() *cobra.Command {
	parent := &cobra.Command{Use: "api-keys", Short: "List SOAR API keys (metadata only; no secret)"}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List API keys: id, name, permission group, SOC role, environments, created",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			keys, err := lc.ListAPIKeys(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(keys)
			}
			fmt.Fprintf(os.Stdout, "%-5s %-34s %-8s %-8s %-12s %s\n", "ID", "NAME", "PERMGRP", "SOCROLE", "CREATED", "ENVIRONMENTS")
			for _, k := range keys {
				created := ""
				if k.CreationTimeMs > 0 {
					created = time.UnixMilli(k.CreationTimeMs).UTC().Format("2006-01-02")
				}
				fmt.Fprintf(os.Stdout, "%-5d %-34s %-8d %-8d %-12s %s\n",
					k.ID, k.Name, k.PermissionGroupID, k.SocRoleID, created, strings.Join(k.Environments, ","))
			}
			fmt.Fprintf(os.Stdout, "\n%d API key(s) — metadata only (the secret is shown only at creation time; `--json` for full scope)\n", len(keys))
			return nil
		},
	}
	parent.AddCommand(listCmd)
	// Bare `api-keys` runs the list.
	parent.RunE = listCmd.RunE
	parent.Args = cobra.NoArgs
	return parent
}

// newSOARPolicyCmd builds a `get`/`set <value>` command pair for one singleton
// policy. value is the integer enum the policy accepts; a set is guarded.
func newSOARPolicyCmd(use, desc, field string,
	get func(*legacy.Client) func(context.Context) (legacy.RawJSON, error),
	set func(*legacy.Client) func(context.Context, any) (legacy.RawJSON, error),
) *cobra.Command {
	parent := &cobra.Command{Use: use, Short: desc}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Print the current " + desc,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			return mirror.PrintSOARSettingSingleton(baseContext(), desc, get(lc), os.Stdout)
		},
	}

	var (
		dryRun bool
		yes    bool
	)
	setCmd := &cobra.Command{
		Use:   "set <value>",
		Short: "Set the " + desc + " (integer enum; guarded)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("value must be an integer enum: %w", err)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard(use+" set", dryRun, yes)
			return mirror.PushSOARSettingPolicy(baseContext(), desc, field, v, set(lc), dr, ay, os.Stdout)
		},
	}
	sf := setCmd.Flags()
	sf.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	sf.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	setCmd.MarkFlagsMutuallyExclusive("dry-run", "yes")

	parent.AddCommand(getCmd, setCmd)
	return parent
}
