package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// The `rule_exclusions` command holds one-off findings-refinement operations
// that sit beside the pull/push reconcile loop.
func init() { rootCmd.AddCommand(newRuleExclusionsCmd()) }

func newRuleExclusionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule_exclusions <verb>",
		Short: "Findings-refinement operations beyond pull/push",
		Long: "Operate on findings refinements outside the reconcile loop. Config-as-code\n" +
			"is `pull rule_exclusions` / `push rule_exclusions`; deployment changes are\n" +
			"guarded and dry-run by default.",
	}
	cmd.AddCommand(newRuleExclusionsDeployCmd())
	return cmd
}

func newRuleExclusionsDeployCmd() *cobra.Command {
	var enable, disable, archive bool
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "deploy <id> (--enable | --disable | --archive)",
		Short: "MUTATING (guarded): enable, disable, or archive a rule exclusion",
		Long: "Patch one findings refinement deployment by id. `--enable` also unarchives\n" +
			"the refinement; `--archive` disables it and marks it archived. Guarded: dry-run\n" +
			"by default, --yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(lastSegment(args[0]))
			if id == "" {
				return fmt.Errorf("rule exclusion id is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ex, err := c.GetRuleExclusion(ctx, id)
			if err != nil {
				return err
			}
			dep, err := c.GetRuleExclusionDeployment(ctx, id)
			if err != nil {
				return err
			}
			upd, desired, err := buildRuleExclusionDeployUpdate(enable, disable, archive, dep)
			if err != nil {
				return err
			}
			display := ex.DisplayName
			if display == "" {
				display = id
			}
			action := fmt.Sprintf("rule_exclusions deploy %q (%s): enabled=%v archived=%v -> %s",
				display, id, dep.Enabled, dep.Archived, desired)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				_, err := c.UpdateRuleExclusionDeployment(ctx, id, upd)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&enable, "enable", false, "enable and unarchive the rule exclusion")
	f.BoolVar(&disable, "disable", false, "disable the rule exclusion")
	f.BoolVar(&archive, "archive", false, "disable and archive the rule exclusion")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable", "archive")
	cmd.MarkFlagsOneRequired("enable", "disable", "archive")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func buildRuleExclusionDeployUpdate(enable, disable, archive bool, current *chronicle.RuleExclusionDeployment) (chronicle.RuleExclusionDeploymentUpdate, string, error) {
	if countTrue(enable, disable, archive) != 1 {
		return chronicle.RuleExclusionDeploymentUpdate{}, "", fmt.Errorf("exactly one of --enable, --disable, or --archive is required")
	}
	upd := chronicle.RuleExclusionDeploymentUpdate{}
	if current != nil {
		upd.Etag = current.Etag
	}
	switch {
	case enable:
		upd.Enabled = new(true)
		upd.Archived = new(false)
		return upd, "enabled=true archived=false", nil
	case disable:
		upd.Enabled = new(false)
		archived := false
		if current != nil {
			archived = current.Archived
		}
		return upd, fmt.Sprintf("enabled=false archived=%v", archived), nil
	case archive:
		upd.Enabled = new(false)
		upd.Archived = new(true)
		return upd, "enabled=false archived=true", nil
	default:
		return chronicle.RuleExclusionDeploymentUpdate{}, "", fmt.Errorf("unreachable deployment update state")
	}
}

func countTrue(vals ...bool) int {
	n := 0
	for _, v := range vals {
		if v {
			n++
		}
	}
	return n
}
