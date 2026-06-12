package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

func newSOARPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push <target>",
		Short: "MUTATING (guarded): live SOAR changes. Dry-run by default",
		Long: "Reconcile local files to live SOAR config. Engine surfaces (" +
			strings.Join(mirror.SOARSurfaceNames(), ", ") + ") create new files,\n" +
			"update edited ones, and (only with --prune) delete server-only objects.",
	}
	cmd.AddCommand(
		newSOARBulkCloseCmd(),
		newSOARPlaybookSaveCmd(),
		newSOARGroupingPushCmd(),
	)
	for _, name := range mirror.SOARSurfaceNames() {
		cmd.AddCommand(newSOAREnginePushCmd(name))
	}
	return cmd
}

// newSOARGroupingPushCmd builds the guarded reconcile push for alert-grouping
// rules. Unlike the engine surfaces it uses the MODERN soar client (the v1alpha
// alertGroupingRules API on the siemplify-soar host), so it is wired separately
// rather than through the legacy engine registry. --prune deletes server-only
// rules but never the non-deletable catch-all fallback (category "ALL").
func newSOARGroupingPushCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
		prune  bool
		out    string
	)
	cmd := &cobra.Command{
		Use:   "grouping [--prune]",
		Short: "Reconcile local alert-grouping rules to live (create/update; --prune to delete, never the fallback)",
		Long: "Reconcile grouping/rules/*.json (alert-grouping rules) to live via the modern\n" +
			"v1alpha alertGroupingRules API. Dry-run by default; --prune deletes server-only\n" +
			"rules but refuses the non-deletable catch-all fallback rule (category \"ALL\").\n" +
			"Edit the General/Overflow settings (grouping/settings.json) in SOAR Settings.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("grouping reconcile", dryRun, yes)
			dir := filepath.Join(mirror.DataRoot(out), mirror.DirSOAR, mirror.DirSOARGrouping, "rules")
			_, err = reconcile.Push(baseContext(), mirror.GroupingRulesSurface(c), dir, reconcile.PushOpts{
				DryRun: dr, AssumeYes: ay, Prune: prune,
			}, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	f.BoolVar(&prune, "prune", false, "also delete server-only rules (never the catch-all fallback)")
	f.StringVar(&out, "out", "", "data root directory (default: cwd)")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

// newSOAREnginePushCmd builds the guarded reconcile push for one engine surface:
// `soar push <surface> [--dry-run|--yes] [--prune]`.
func newSOAREnginePushCmd(name string) *cobra.Command {
	var (
		dryRun bool
		yes    bool
		prune  bool
		out    string
	)
	// Read the surface capabilities once (nil client) to make the help and the
	// --prune handling capability-aware: --prune only deletes on a PruneEligible
	// surface, so anything else (NoDelete or not-eligible) is a no-op.
	caps, _ := surfaceCaps(name)
	short := "Reconcile local " + name + " files to live (create/update; --prune to delete)"
	pruneHelp := "also delete live objects with no local file (guarded; gated on a complete pull)"
	if reason, noop := pruneNoOp(caps); noop {
		short = "Reconcile local " + name + " files to live (create/update; --prune is a no-op: " + reason + ")"
		pruneHelp = "no-op for this surface (" + reason + "); live-only objects are reported, never deleted"
	}
	cmd := &cobra.Command{
		Use:   name + " [--prune]",
		Short: short,
		Long:  short + "\n" + surfaceNote(name),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			s, ok := mirror.BuildSOARSurface(name, lc)
			if !ok {
				return fmt.Errorf("unknown engine surface %q", name)
			}
			if prune && !jsonOut {
				if reason, noop := pruneNoOp(s.Caps); noop {
					fmt.Fprintf(os.Stdout, "note: --prune is a no-op for %q (%s); "+
						"live-only objects are reported, never deleted\n", name, reason)
				}
			}
			// Apply the same .secopsctl-redact masking pull uses, so the live
			// object canonicalizes to the redacted form and a masked value is not
			// seen as a diff (and the marker guard refuses to deploy a mask).
			if err := applyValueRedaction(mirror.DataRoot(out), nil); err != nil {
				return err
			}
			dr, ay := soarGuard(name+" reconcile", dryRun, yes)
			dir := filepath.Join(mirror.DataRoot(out), mirror.DirSOAR, s.Dir)
			_, err = reconcile.Push(baseContext(), s, dir, reconcile.PushOpts{
				DryRun: dr, AssumeYes: ay, Prune: prune,
			}, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	f.BoolVar(&prune, "prune", false, pruneHelp)
	f.StringVar(&out, "out", "", "data root directory (default: cwd)")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	// NOTE: not markJSON'd. The only jsonOut reference here suppresses a prune
	// note; reconcile.Push still writes human text to stdout (no JSON output), so
	// `soar push <surface>` does not honor --json.
	return cmd
}

func newSOARBulkCloseCmd() *cobra.Command {
	var (
		idsArg    string
		reason    string
		rootCause string
		comment   string
		dryRun    bool
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "bulk-close",
		Short: "Bulk-close SOAR cases by id (reason: malicious|not-malicious|maintenance|inconclusive|unknown)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseIntList(idsArg)
			if err != nil {
				return err
			}
			cr, err := parseCloseReason(reason)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("bulk-close cases", dryRun, yes)
			_, err = mirror.PushSOARBulkClose(baseContext(), lc, ids, cr, rootCause, comment, dr, ay, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&idsArg, "ids", "", "comma-separated SOAR case ids (required)")
	f.StringVar(&reason, "reason", "maintenance", "close reason: malicious | not-malicious | maintenance | inconclusive | unknown")
	f.StringVar(&rootCause, "root-cause", "", "close root cause")
	f.StringVar(&comment, "comment", "", "close comment")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}

func newSOARPlaybookSaveCmd() *cobra.Command {
	var (
		file   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "playbook --file <playbook.json>",
		Short: "Save ONE playbook from a file (whole-body replace; mints a new version)",
		Long: "Save a single playbook definition from --file (imperative whole-body replace;\n" +
			"the server mints a new version). This is NOT the directory reconcile — for that\n" +
			"use `soar push playbooks` (plural), which diffs the local playbooks/ folder\n" +
			"against live and supports --prune. Singular = save one file; plural = reconcile.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook save", dryRun, yes)
			return mirror.PushSOARPlaybookSave(baseContext(), lc, file, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "playbook JSON to save (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// parseCloseReason maps a reason name (or a raw int) to the typed CloseReason. Names
// are used over magic ints because the integer coding is the server's and is NOT
// alphabetical (Malicious=0) — a bare number is easy to get backwards.
func parseCloseReason(s string) (legacy.CloseReason, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "malicious":
		return legacy.CloseMalicious, nil
	case "not-malicious", "notmalicious", "not_malicious":
		return legacy.CloseNotMalicious, nil
	case "maintenance":
		return legacy.CloseMaintenance, nil
	case "inconclusive":
		return legacy.CloseInconclusive, nil
	case "unknown":
		return legacy.CloseUnknown, nil
	}
	// A raw integer is accepted only when it is one of the server's defined
	// codings — an arbitrary number would otherwise reach the wire as the Go
	// fallback string "CloseReason(N)".
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		switch r := legacy.CloseReason(n); r {
		case legacy.CloseMalicious, legacy.CloseNotMalicious, legacy.CloseMaintenance,
			legacy.CloseInconclusive, legacy.CloseUnknown:
			return r, nil
		}
	}
	return 0, fmt.Errorf("invalid close reason %q (use malicious|not-malicious|maintenance|inconclusive|unknown)", s)
}
