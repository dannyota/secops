package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
)

// push flag values (per-invocation; cobra resets them each parse).
var (
	pushDryRun   bool   // --dry-run
	pushYes      bool   // --yes
	pushPrune    bool   // --prune (engine surfaces only)
	pushRulesDir string // --rules-dir
	pushRule     string // --rule (rules-deploy only)
	pushOut      string // --out (data root; default cwd)

	pushRuleCreateEnabled      bool
	pushRuleCreateAlerting     bool
	pushRuleCreateRunFrequency string
)

func init() {
	engine := mirror.SIEMSurfaceNames()
	valid := append([]string{"rules-create", "rules-update", "rules-deploy", "rules-disable", "curated"}, engine...)

	pushCmd := &cobra.Command{
		Use:   "push <target>",
		Short: "MUTATING (guarded): create/disable rules, reconcile config. Dry-run by default",
		Long: "Push changes to the LIVE tenant. Defaults to a dry run; pass --yes to\n" +
			"actually apply (or confirm interactively). Exit codes: 0 ok · 1 error\n" +
			"(`drift` reports divergence with exit 2 without mutating).\n\n" +
			"Targets:\n" +
			"  rules-create     create live rules from *.yaral with no companion *.yaml\n" +
			"  rules-update     update live YARA-L text where a tracked *.yaral changed (etag-guarded)\n" +
			"  rules-deploy     reconcile each tracked rule's deployment (enabled/alerting/frequency)\n" +
			"  rules-disable    disable locally-tracked rules with deployment.enabled=true\n" +
			"  curated          reconcile curated/deployments.yaml (enabled/alerting)\n" +
			"  " + strings.Join(engine, ", ") + "   reconcile local files to live (create/update; --prune to delete)\n\n" +
			"Not every surface is prune-eligible; run `secopsctl surfaces` to see which\n" +
			"`--prune` can delete (the rest report orphans but never delete them).",
		Example: "  # preview creating new rules (dry run — the default)\n" +
			"  secopsctl push rules-create\n\n" +
			"  # apply for real after reviewing the preview\n" +
			"  secopsctl push rules-create --yes\n\n" +
			"  # reconcile an engine surface, deleting live objects with no local file\n" +
			"  secopsctl push connectors --prune --dry-run",
		Args:      cobra.ExactArgs(1),
		ValidArgs: valid,
		RunE:      runPush,
	}
	f := pushCmd.Flags()
	f.BoolVar(&pushDryRun, "dry-run", false,
		"preview only; never mutate (this is the default behavior)")
	f.BoolVar(&pushYes, "yes", false,
		"skip the interactive confirmation and apply the change for real")
	f.BoolVar(&pushPrune, "prune", false,
		"engine surfaces: also delete live objects with no local file (guarded; "+
			"a no-op on surfaces without a delete API — see `push <target> --help`)")
	f.StringVar(&pushRulesDir, "rules-dir", "",
		"directory of local rule files (default: <dataRoot>/rules)")
	f.StringVar(&pushRule, "rule", "",
		"rules-deploy: limit deployment reconciliation to one rule id, display name, or slug")
	f.StringVar(&pushOut, "out", "",
		"data root directory the engine surfaces read from (default: cwd; matches pull/drift)")
	f.BoolVar(&pushRuleCreateEnabled, "enabled", true,
		"rules-create: initial deployment enabled state")
	f.BoolVar(&pushRuleCreateAlerting, "alerting", true,
		"rules-create: initial deployment alerting state")
	f.StringVar(&pushRuleCreateRunFrequency, "run-frequency", mirror.DefaultRulesCreateDeploymentOptions().RunFrequency,
		"rules-create: initial run frequency (LIVE, HOURLY, or DAILY)")
	// --dry-run and --yes are conceptually opposed; --dry-run always wins (see
	// the dryRun/assumeYes derivation below), mirroring the legacy tool.
	pushCmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	// `push <target> --help` appends the target's write semantics + prune/etag caps.
	attachTargetHelp(pushCmd, valid)

	rootCmd.AddCommand(markJSON(pushCmd))
}

// runPush dispatches a guarded mutating push.
//
// Guard semantics (identical to the legacy Python CLI):
//   - dry-run is the default and is implied unless --yes is given.
//   - --dry-run forces preview-only even alongside --yes.
//
// So: dryRun = --dry-run || !--yes ; assumeYes = --yes && !--dry-run.
//
// When a real mutation is requested (dryRun==false) and assumeYes is not set, we
// only proceed if stdin is an interactive TTY and the operator answers y/N here;
// the mirror layer itself is non-interactive and refuses on assumeYes==false.
func runPush(cmd *cobra.Command, args []string) error {
	target := args[0]

	dryRun := pushDryRun || !pushYes
	assumeYes := pushYes && !pushDryRun
	// Read-only mode degrades every push to a dry run, --yes or not. The notice
	// and audit record are written by maybeConfirm — AFTER target/data-dir
	// validation — so a push that could never have run leaves no phantom record.
	readOnly := readOnlyMode()
	if readOnly {
		dryRun, assumeYes = true, false
	}

	client, err := newChronicleClient()
	if err != nil {
		return err
	}
	ctx := baseContext()

	// For a live mutation without --yes, offer an interactive confirmation when a
	// TTY is attached — called only AFTER the data-dir check below, so the operator
	// is never asked to confirm a push that is then refused. A "yes" promotes the
	// run to assumeYes; anything else (incl. no TTY / --non-interactive) leaves it
	// false so the mirror layer aborts cleanly.
	maybeConfirm := func() {
		if readOnly {
			if pushYes {
				noteReadOnly("push " + target)
				auditMutation("push "+target, "read-only")
			}
			return
		}
		if !dryRun && !assumeYes && confirmPush(target) {
			assumeYes = true
		}
		if assumeYes {
			auditMutation("push "+target, "confirmed")
		}
	}

	// Engine-backed SIEM surfaces (reference_lists, …): reconcile local files to
	// live (create/update; --prune to delete) through the shared reconcile engine.
	out := io.Writer(os.Stdout)
	if jsonOut {
		out = io.Discard // suppress human text; emit JSON below
	}

	if s, ok := mirror.BuildSIEMSurface(target, client); ok {
		dir := filepath.Join(mirror.DataRoot(pushOut), s.Dir)
		// Compare each dashboard in its own shape: an inline mirror file gets its
		// live side dereferenced; a reference-only file is compared as references.
		if target == "dashboards" {
			s = mirror.DashboardsSurfaceForMirror(client, dir)
		}
		if err := ensureDataDir(target, dir, dryRun); err != nil {
			return err
		}
		// Say up front when --prune can't delete on this surface, so the operator
		// isn't left expecting a sweep the engine will silently skip (it prunes only
		// PruneEligible surfaces — both NoDelete and not-eligible are no-ops).
		if pushPrune && !jsonOut {
			if reason, noop := pruneNoOp(s.Caps); noop {
				fmt.Fprintf(out, "note: --prune is a no-op for %q (%s); "+
					"live-only objects are reported, never deleted\n", target, reason)
			}
		}
		maybeConfirm()
		sum, perr := reconcile.Push(ctx, s, dir, reconcile.PushOpts{
			DryRun: dryRun, AssumeYes: assumeYes, Prune: pushPrune,
		}, out)
		if perr != nil {
			return perr
		}
		if jsonOut {
			return emitJSON(struct {
				Target      string                 `json:"target"`
				DryRun      bool                   `json:"dry_run"`
				Applied     bool                   `json:"applied"`
				Created     int                    `json:"created"`
				Updated     int                    `json:"updated"`
				Deleted     int                    `json:"deleted"`
				Unchanged   int                    `json:"unchanged"`
				Failed      int                    `json:"failed"`
				Skipped     int                    `json:"skipped_deletes"`
				SkipReason  string                 `json:"skip_reason,omitempty"`
				WouldChange bool                   `json:"would_change"`
				Items       []reconcile.PlanChange `json:"items,omitempty"`
			}{
				Target: target, DryRun: dryRun, Applied: !dryRun && assumeYes,
				Created: sum.Created, Updated: sum.Updated, Deleted: sum.Deleted,
				Unchanged: sum.Unchanged, Failed: sum.Failed,
				Skipped: len(sum.SkippedDeletes), SkipReason: sum.SkipReason,
				WouldChange: sum.Created+sum.Updated+sum.Deleted > 0,
				Items:       sum.Changes,
			})
		}
		return nil
	}

	switch target {
	case "rules-create", "rules-update", "rules-deploy", "rules-disable", "curated":
	default:
		return fmt.Errorf("unknown push target %q (want one of: %s)",
			target, strings.Join(append([]string{"rules-create", "rules-update", "rules-deploy", "rules-disable", "curated"}, mirror.SIEMSurfaceNames()...), ", "))
	}
	if pushRule != "" && target != "rules-deploy" {
		return fmt.Errorf("--rule only applies to `push rules-deploy`")
	}
	if target != "rules-create" && rulesCreateFlagChanged(cmd) {
		return fmt.Errorf("--enabled, --alerting, and --run-frequency only apply to `push rules-create`")
	}
	if target == "curated" {
		dir := filepath.Join(mirror.DataRoot(pushOut), mirror.DirCurated)
		if err := ensureDataDir(target, dir, dryRun); err != nil {
			return err
		}
		maybeConfirm()
		n, err := mirror.PushCuratedDeployments(ctx, client, dir, dryRun, assumeYes, out)
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(struct {
				Target  string `json:"target"`
				DryRun  bool   `json:"dry_run"`
				Applied bool   `json:"applied"`
				Count   int    `json:"count"`
			}{Target: target, DryRun: dryRun, Applied: !dryRun && assumeYes, Count: n})
		}
		return nil
	}
	rulesDir := pushRulesDir
	if rulesDir == "" {
		rulesDir = filepath.Join(mirror.DataRoot(pushOut), mirror.DirRules)
	}
	if err := ensureDataDir(target, rulesDir, dryRun); err != nil {
		return err
	}
	maybeConfirm()
	var n int
	switch target {
	case "rules-create":
		n, err = mirror.PushRulesCreateWithOptions(ctx, client, rulesDir, mirror.RulesCreateDeploymentOptions{
			Enabled:      pushRuleCreateEnabled,
			Alerting:     pushRuleCreateAlerting,
			RunFrequency: pushRuleCreateRunFrequency,
		}, dryRun, assumeYes, out)
	case "rules-update":
		n, err = mirror.PushRulesUpdate(ctx, client, rulesDir, dryRun, assumeYes, out)
	case "rules-deploy":
		n, err = mirror.PushRulesDeployFiltered(ctx, client, rulesDir, pushRule, dryRun, assumeYes, out)
	case "rules-disable":
		n, err = mirror.PushRulesDisable(ctx, client, rulesDir, dryRun, assumeYes, out)
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(struct {
			Target  string `json:"target"`
			DryRun  bool   `json:"dry_run"`
			Applied bool   `json:"applied"`
			Count   int    `json:"count"`
		}{Target: target, DryRun: dryRun, Applied: !dryRun && assumeYes, Count: n})
	}
	return err
}

func rulesCreateFlagChanged(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("enabled") ||
		cmd.Flags().Changed("alerting") ||
		cmd.Flags().Changed("run-frequency")
}

// confirmPush prompts the operator for an interactive y/N confirmation. It
// returns true only on an explicit "y"/"yes". If stdin is not a TTY it skips the
// prompt and returns false, so non-interactive runs fall through to the mirror
// layer's abort-without-confirmation path.
func confirmPush(target string) bool {
	// Never prompt in non-interactive or --json mode (a y/N prompt on stdout would
	// corrupt machine-readable output); the mutation is then refused without --yes.
	if nonInteractive || jsonOut || !stdinIsTerminal() {
		return false
	}
	fmt.Fprintf(os.Stdout, "Apply LIVE %q to the production tenant? [y/N] ", target)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// ensureDataDir refuses a LIVE push when the resolved local data directory does
// not exist — almost always a wrong --out or working directory, which (with
// --prune) would read zero local files and delete live objects. A dry run is
// always allowed: it previews and mutates nothing.
func ensureDataDir(target, dir string, dryRun bool) error {
	if dryRun {
		return nil
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("push %s: local data dir %q not found — pass --out <data root> or run from it "+
			"(refusing a live push that would read no local files)", target, dir)
	}
	return nil
}

// stdinIsTerminal reports whether stdin is an interactive character device,
// using only the standard library (no golang.org/x/term dependency).
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
