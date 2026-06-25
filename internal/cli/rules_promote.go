package cli

import (
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
)

// newRulesPromoteCmd ships a brand-new rule in one guarded step: validate the
// YARA-L file, create the live rule, and deploy it to the requested initial state
// — the create + deploy that otherwise takes two commands.
func newRulesPromoteCmd() *cobra.Command {
	var (
		enabled, alerting bool
		runFrequency      string
		dryRun, yes       bool
	)
	cmd := &cobra.Command{
		Use:   "promote <file.yaral>",
		Short: "MUTATING (guarded): create a new rule from a file and deploy it in one step",
		Long: "Validate a YARA-L file, create the live rule, and deploy it to the requested\n" +
			"initial state — the single-step \"ship a rule\" path that otherwise needs\n" +
			"`push rules-create` then `push rules-deploy`. The file must NOT already have a\n" +
			"companion .yaml (that rule is tracked — use rules-update / rules-deploy). The\n" +
			"companion .yaml is written next to the file on success. Multi-event rules that\n" +
			"cannot run LIVE fall back to HOURLY to preserve enabled=true. Guarded: dry-run\n" +
			"by default, --yes to apply.",
		Example: "  secopsctl rules promote detections/new-rule.yaral --dry-run\n" +
			"  secopsctl rules promote detections/new-rule.yaral --alerting=false --yes",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			dr, ay := deriveGuard("rules promote "+args[0], dryRun, yes)
			opts := mirror.RulesCreateDeploymentOptions{
				Enabled:      enabled,
				Alerting:     alerting,
				RunFrequency: runFrequency,
			}
			_, err = mirror.PromoteRule(baseContext(), c, args[0], opts, dr, ay, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&enabled, "enabled", true, "initial deployment enabled state")
	f.BoolVar(&alerting, "alerting", true, "initial deployment alerting state")
	f.StringVar(&runFrequency, "run-frequency", mirror.DefaultRulesCreateDeploymentOptions().RunFrequency,
		"initial run frequency: LIVE | HOURLY | DAILY")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
