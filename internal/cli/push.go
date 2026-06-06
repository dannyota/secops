package cli

import (
	"bufio"
	"fmt"
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
)

func init() {
	engine := mirror.SIEMSurfaceNames()
	valid := append([]string{"rules-create", "rules-update", "rules-deploy", "rules-disable"}, engine...)

	pushCmd := &cobra.Command{
		Use:   "push <target>",
		Short: "MUTATING (guarded): create/disable rules, reconcile config. Dry-run by default",
		Long: "Push changes to the LIVE tenant. Defaults to a dry run; pass --yes to\n" +
			"actually apply (or confirm interactively).\n\n" +
			"Targets:\n" +
			"  rules-create     create live rules from *.yaral with no companion *.yaml\n" +
			"  rules-update     update live YARA-L text where a tracked *.yaral changed (etag-guarded)\n" +
			"  rules-deploy     reconcile each tracked rule's deployment (enabled/alerting/frequency)\n" +
			"  rules-disable    disable locally-tracked rules with deployment.enabled=true\n" +
			"  " + strings.Join(engine, ", ") + "   reconcile local files to live (create/update; --prune to delete)",
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
		"engine surfaces: also delete live objects with no local file (guarded)")
	f.StringVar(&pushRulesDir, "rules-dir", "",
		"directory of local rule files (default: <dataRoot>/rules)")
	// --dry-run and --yes are conceptually opposed; --dry-run always wins (see
	// the dryRun/assumeYes derivation below), mirroring the legacy tool.
	pushCmd.MarkFlagsMutuallyExclusive("dry-run", "yes")

	rootCmd.AddCommand(pushCmd)
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

	// For a live mutation without --yes, offer an interactive confirmation when a
	// TTY is attached. A "yes" here promotes the run to assumeYes; anything else
	// (including no TTY) leaves assumeYes false so the mirror layer aborts cleanly.
	if !dryRun && !assumeYes {
		if confirmPush(target) {
			assumeYes = true
		}
	}

	client, err := newChronicleClient()
	if err != nil {
		return err
	}
	ctx := baseContext()

	// Engine-backed SIEM surfaces (reference_lists, …): reconcile local files to
	// live (create/update; --prune to delete) through the shared reconcile engine.
	if s, ok := mirror.BuildSIEMSurface(target, client); ok {
		dir := filepath.Join(mirror.DataRoot(""), s.Dir)
		_, err = reconcile.Push(ctx, s, dir, reconcile.PushOpts{
			DryRun: dryRun, AssumeYes: assumeYes, Prune: pushPrune,
		}, os.Stdout)
		return err
	}

	rulesDir := pushRulesDir
	if rulesDir == "" {
		rulesDir = filepath.Join(mirror.DataRoot(""), mirror.DirRules)
	}
	switch target {
	case "rules-create":
		_, err = mirror.PushRulesCreate(ctx, client, rulesDir, dryRun, assumeYes, os.Stdout)
	case "rules-update":
		_, err = mirror.PushRulesUpdate(ctx, client, rulesDir, dryRun, assumeYes, os.Stdout)
	case "rules-deploy":
		_, err = mirror.PushRulesDeploy(ctx, client, rulesDir, dryRun, assumeYes, os.Stdout)
	case "rules-disable":
		_, err = mirror.PushRulesDisable(ctx, client, rulesDir, dryRun, assumeYes, os.Stdout)
	default:
		return fmt.Errorf("unknown push target %q (want one of: %s)",
			target, strings.Join(append([]string{"rules-create", "rules-update", "rules-deploy", "rules-disable"}, mirror.SIEMSurfaceNames()...), ", "))
	}
	return err
}

// confirmPush prompts the operator for an interactive y/N confirmation. It
// returns true only on an explicit "y"/"yes". If stdin is not a TTY it skips the
// prompt and returns false, so non-interactive runs fall through to the mirror
// layer's abort-without-confirmation path.
func confirmPush(target string) bool {
	if !stdinIsTerminal() {
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

// stdinIsTerminal reports whether stdin is an interactive character device,
// using only the standard library (no golang.org/x/term dependency).
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
