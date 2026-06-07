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
	)
	for _, name := range mirror.SOARSurfaceNames() {
		cmd.AddCommand(newSOAREnginePushCmd(name))
	}
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
	cmd := &cobra.Command{
		Use:   name + " [--prune]",
		Short: "Reconcile local " + name + " files to live (create/update; --prune to delete)",
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
	f.BoolVar(&prune, "prune", false, "also delete live objects with no local file (guarded; gated on a complete pull)")
	f.StringVar(&out, "out", "", "data root directory (default: cwd)")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
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
		Short: "Save a playbook definition (whole-body replace; mints a new version)",
		Args:  cobra.NoArgs,
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
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return legacy.CloseReason(n), nil
	}
	return 0, fmt.Errorf("invalid close reason %q (use malicious|not-malicious|maintenance|inconclusive|unknown)", s)
}
