package cli

import (
	"context"
	"crypto/rand"
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

// newRandomUUID mints a random (version 4) UUID from crypto/rand — the
// client-generated secret value an API-key create stores server-side.
func newRandomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mint key value: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

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

// newSOARAPIKeysCmd manages the SOAR external-API keys. `list` is read-only
// metadata (the server masks the secret; the typed view drops it). `create`
// MINTS the secret client-side (the server stores whatever value is supplied
// and only ever returns it masked afterwards), prints it exactly once, and
// never persists or logs it (House Rule 4). `revoke` posts the full record
// back, resolved by --name or --id from the list.
func newSOARAPIKeysCmd() *cobra.Command {
	parent := &cobra.Command{Use: "api-keys", Short: "List, create (guarded), and revoke (guarded) SOAR API keys"}
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
	markJSON(listCmd)
	parent.AddCommand(listCmd, newSOARAPIKeyCreateCmd(), newSOARAPIKeyRevokeCmd())
	// Bare `api-keys` runs the list.
	parent.RunE = listCmd.RunE
	parent.Args = cobra.NoArgs
	return markJSON(parent)
}

func newSOARAPIKeyCreateCmd() *cobra.Command {
	var (
		name            string
		permissionGroup int
		socRole         int
		environments    []string
		dryRun, yes     bool
	)
	cmd := &cobra.Command{
		Use:   "create --name <n> --permission-group N",
		Short: "MUTATING (guarded): create an API key — the secret is minted locally and printed ONCE",
		Long: "Register a new SOAR external-API key. The secret value is generated locally\n" +
			"(crypto/rand UUID — the server stores the supplied value and only ever shows\n" +
			"it masked afterwards), printed exactly once on success, and never persisted\n" +
			"or logged. Pick the least-privileged --permission-group for the key's job\n" +
			"(`soar pull soc-roles` and the web UI's Settings list the groups).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			action := fmt.Sprintf("create API key %q (permission group %d)", name, permissionGroup)
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				fmt.Fprintf(os.Stdout, "DRY RUN: would %s, environments %s; a fresh secret is minted locally and printed once on apply.\n",
					action, strings.Join(environments, ","))
				return nil
			}
			secret, err := newRandomUUID()
			if err != nil {
				return err
			}
			if err := lc.CreateAPIKey(baseContext(), name, secret, permissionGroup, socRole, environments); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "created API key %q.\n\n  AppKey: %s\n\nThis value is shown ONCE and cannot be retrieved again — store it in a secret manager now.\n", name, secret)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "key name (required)")
	f.IntVar(&permissionGroup, "permission-group", 0, "permission group id the key acts as (required)")
	f.IntVar(&socRole, "soc-role", 0, "SOC role id (0 = server default)")
	f.StringSliceVar(&environments, "environment", []string{"*"}, "environment scope (repeatable; default all)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("permission-group")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
}

func newSOARAPIKeyRevokeCmd() *cobra.Command {
	var (
		name        string
		id          int
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "revoke (--name <n> | --id N)",
		Short: "MUTATING (guarded): revoke an API key — the credential stops working immediately",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (strings.TrimSpace(name) == "") == (id == 0) {
				return fmt.Errorf("exactly one of --name or --id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			keys, err := lc.ListAPIKeys(baseContext())
			if err != nil {
				return err
			}
			var match *legacy.APIKey
			for i := range keys {
				if (id != 0 && keys[i].ID == id) || (id == 0 && keys[i].Name == name) {
					if match != nil {
						return fmt.Errorf("name %q matches several keys — use --id", name)
					}
					match = &keys[i]
				}
			}
			if match == nil {
				return fmt.Errorf("no API key matches (see `api-keys list`)")
			}
			action := fmt.Sprintf("revoke API key id %d %q", match.ID, match.Name)
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				fmt.Fprintf(os.Stdout, "DRY RUN: would %s — any client using it loses access immediately.\n", action)
				return nil
			}
			if err := lc.RevokeAPIKey(baseContext(), *match); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "revoked API key id %d %q.\n", match.ID, match.Name)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "key name to revoke")
	f.IntVar(&id, "id", 0, "key id to revoke")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
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
