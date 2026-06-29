package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar"
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
		where     string
		reason    string
		rootCause string
		comment   string
		dryRun    bool
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "bulk-close",
		Short: "Bulk-close SOAR cases by id or by a filter (reason: malicious|not-malicious|maintenance|inconclusive|unknown)",
		Long: "Close many SOAR cases in one guarded command with a typed close-reason, root\n" +
			"cause, and comment. Select the cases by explicit ids (--ids) or by a modern\n" +
			"cases-list filter (--where), e.g. the stale/duplicate sets a case-hygiene job\n" +
			"would sweep. With --where, the matching cases are listed and counted before\n" +
			"the guard so the set is reviewable. Dry-run by default; --yes to apply.",
		Example: "  # by id\n" +
			"  secopsctl soar push bulk-close --ids 101,102 --reason not-malicious\n\n" +
			"  # by filter: close a stale set with a root cause, reviewed first\n" +
			"  secopsctl soar push bulk-close \\\n" +
			"      --where \"status = 'OPENED' and priority = 'PRIORITY_LOW'\" \\\n" +
			"      --reason maintenance --root-cause 'Auto-closed: stale'",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (idsArg == "") == (where == "") {
				return fmt.Errorf("exactly one of --ids or --where is required")
			}
			cr, err := parseCloseReason(reason)
			if err != nil {
				return err
			}
			var ids []int
			if idsArg != "" {
				if ids, err = parseIntList(idsArg); err != nil {
					return err
				}
			} else {
				if ids, err = resolveCaseIDsByFilter(baseContext(), where); err != nil {
					return err
				}
				if len(ids) == 0 {
					fmt.Fprintf(os.Stdout, "no cases match the filter — nothing to close.\n")
					return nil
				}
				fmt.Fprintf(os.Stdout, "filter matched %d case(s): %s\n", len(ids), joinInts(ids))
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
	f.StringVar(&idsArg, "ids", "", "comma-separated SOAR case ids (one of --ids/--where required)")
	f.StringVar(&where, "where", "", "modern cases-list filter selecting the cases to close (alternative to --ids)")
	f.StringVar(&reason, "reason", "maintenance", "close reason: malicious | not-malicious | maintenance | inconclusive | unknown")
	f.StringVar(&rootCause, "root-cause", "", "close root cause")
	f.StringVar(&comment, "comment", "", "close comment")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("ids", "where")
	return cmd
}

// resolveCaseIDsByFilter lists cases matching a modern cases-list filter and
// returns their integer SOAR ids (the last segment of each case resource name) —
// the id form the legacy bulk-close endpoint takes.
func resolveCaseIDsByFilter(ctx context.Context, filter string) ([]int, error) {
	c, err := newSOARClient()
	if err != nil {
		return nil, err
	}
	cases, err := c.ListCasesTyped(ctx, soar.CaseListOptions{Filter: filter})
	if err != nil {
		return nil, fmt.Errorf("resolve --where: %w", err)
	}
	ids := make([]int, 0, len(cases))
	for i := range cases {
		id, err := soarCaseIntID(cases[i].Name)
		if err != nil {
			return nil, fmt.Errorf("case %q: %w", cases[i].Name, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// soarCaseIntID parses the integer case id from a case resource name
// (…/cases/<id>) or a bare numeric string.
func soarCaseIntID(name string) (int, error) {
	seg := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		seg = name[i+1:]
	}
	return strconv.Atoi(strings.TrimSpace(seg))
}

// joinInts renders an int slice as a comma-separated string for previews.
func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
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
