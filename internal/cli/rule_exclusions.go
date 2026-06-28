package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func init() {
	rootCmd.AddCommand(newRuleExclusionsCmd())
}

// newRuleExclusionsCmd is a top-level group (`exclusions …`) — findings
// refinements filter noise out of BOTH custom (`rules`) and Google-managed
// (`curated`) detections, so they sit beside both rather than under either.
func newRuleExclusionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exclusions <verb>",
		Short: "Imperative findings-refinement ops (enable/disable/archive) for custom + curated detections — config-as-code is `pull/push rule_exclusions`",
		Long: "Operate on findings refinements outside the reconcile loop. Exclusions apply\n" +
			"to both custom (`rules`) and Google-managed (`curated`) detections.\n" +
			"Config-as-code is `pull rule_exclusions` / `push rule_exclusions`; deployment\n" +
			"changes are guarded and dry-run by default.",
	}
	cmd.AddCommand(newRuleExclusionsDeployCmd(), newRuleExclusionsListCmd(), newRuleExclusionsGetCmd())
	return cmd
}

func newRuleExclusionsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list rule exclusions (findings refinements) with id, type, query",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			xs, err := c.ListRuleExclusions(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(xs)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tDISPLAY NAME\tTYPE\tQUERY")
			for i := range xs {
				x := &xs[i]
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", x.ID(), orDash(x.DisplayName), string(x.Type), truncate(x.Query, 50))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d rule exclusion(s).\n", len(xs))
			return nil
		},
	}
	return markJSON(cmd)
}

func newRuleExclusionsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Read-only: get one rule exclusion (incl. its deployment state)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			x, err := c.GetRuleExclusion(baseContext(), args[0])
			if err != nil {
				return err
			}
			return emitJSON(x)
		},
	}
	return markJSON(cmd)
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
	return markJSON(cmd)
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
