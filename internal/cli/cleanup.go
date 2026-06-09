package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror"
)

func init() { rootCmd.AddCommand(newCleanupCmd()) }

func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup <verb>",
		Short: "Guarded cleanup helpers for secopsctl-owned artifacts",
		Long: "Cleanup helpers for artifacts created by secopsctl smoke tests. Commands\n" +
			"target only secopsctl-owned smoke prefixes and are dry-run by default.",
	}
	cmd.AddCommand(newCleanupSmokeArtifactsCmd())
	return cmd
}

func newCleanupSmokeArtifactsCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "smoke-artifacts",
		Short: "MUTATING (guarded): remove or neutralize secopsctl smoke artifacts",
		Long: "Find SIEM resources whose names start with secopsctl smoke-test prefixes,\n" +
			"then delete surfaces with a clean delete API, archive smoke rule exclusions,\n" +
			"and empty smoke reference lists. The command previews the plan first and\n" +
			"never targets names outside the secopsctl-owned smoke prefixes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			items, warnings := buildSmokeCleanupPlan(ctx, c)
			dry := dryRun || !yes
			applied := false
			if jsonOut {
				if !dry && yes {
					if err := applySmokeCleanup(items); err != nil {
						return err
					}
					applied = true
				}
				return emitJSON(struct {
					DryRun      bool               `json:"dry_run"`
					Applied     bool               `json:"applied"`
					Items       []smokeCleanupItem `json:"items"`
					Warnings    []string           `json:"warnings,omitempty"`
					WouldChange bool               `json:"would_change"`
				}{DryRun: dry, Applied: applied, Items: items, Warnings: warnings, WouldChange: len(items) > 0})
			}
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", w)
			}
			if len(items) == 0 {
				fmt.Fprintln(os.Stdout, "No secopsctl smoke artifacts found.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "Smoke cleanup plan (%d item(s)):\n", len(items))
			for _, it := range items {
				fmt.Fprintf(os.Stdout, "  - %-18s %-10s %s\n", it.Surface, it.Action, it.Target)
			}
			action := fmt.Sprintf("cleanup smoke-artifacts: %d secopsctl-owned item(s)", len(items))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				return applySmokeCleanup(items)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

type smokeCleanupItem struct {
	Surface string `json:"surface"`
	Action  string `json:"action"`
	Target  string `json:"target"`
	apply   func() error
}

func buildSmokeCleanupPlan(ctx context.Context, c *chronicle.Client) ([]smokeCleanupItem, []string) {
	var items []smokeCleanupItem
	var warnings []string
	addWarning := func(surface string, err error) {
		warnings = append(warnings, fmt.Sprintf("cleanup %s: %v", surface, err))
	}

	rules, err := c.ListRules(ctx)
	if err != nil {
		addWarning("rules", err)
	} else {
		for i := range rules {
			r := rules[i]
			ruleID := r.RuleID()
			if ruleID == "" || !isSecopsctlSmokeName(r.DisplayName) {
				continue
			}
			target := fmt.Sprintf("%s (%s)", r.DisplayName, ruleID)
			items = append(items, smokeCleanupItem{
				Surface: "rules",
				Action:  "delete",
				Target:  target,
				apply: func() error {
					return c.DeleteRule(ctx, ruleID, true)
				},
			})
		}
	}

	for _, surface := range []string{"data_tables", "dashboards", "feeds", "forwarders", "datataps", "scheduled_reports", "error_notifications", "federation_groups"} {
		s, ok := mirror.BuildSIEMSurface(surface, c)
		if !ok || s.Delete == nil {
			continue
		}
		res, err := s.List(ctx)
		if err != nil {
			addWarning(surface, err)
			continue
		}
		for i := range res.Objects {
			o := res.Objects[i]
			if !isSecopsctlSmokeName(o.Slug) && !isSecopsctlSmokeName(lastSegment(o.ServerID)) {
				continue
			}
			target := fmt.Sprintf("%s (%s)", o.Slug, lastSegment(o.ServerID))
			items = append(items, smokeCleanupItem{
				Surface: surface,
				Action:  "delete",
				Target:  target,
				apply: func() error {
					return s.Delete(ctx, o)
				},
			})
		}
	}

	lists, err := c.ListReferenceLists(ctx)
	if err != nil {
		addWarning("reference_lists", err)
	} else {
		for i := range lists {
			rl := lists[i]
			if !isSecopsctlSmokeName(rl.DisplayName) && !isSecopsctlSmokeName(lastSegment(rl.Name)) {
				continue
			}
			if len(rl.Entries) == 0 {
				continue
			}
			target := fmt.Sprintf("%s (%d entries)", rl.DisplayName, len(rl.Entries))
			items = append(items, smokeCleanupItem{
				Surface: "reference_lists",
				Action:  "empty",
				Target:  target,
				apply: func() error {
					_, err := c.UpdateReferenceList(ctx, rl.Name, "", []string{})
					return err
				},
			})
		}
	}

	exclusions, err := c.ListRuleExclusions(ctx)
	if err != nil {
		addWarning("rule_exclusions", err)
	} else {
		for i := range exclusions {
			ex := exclusions[i]
			if !isSecopsctlSmokeName(ex.DisplayName) && !isSecopsctlSmokeName(lastSegment(ex.Name)) {
				continue
			}
			id := ex.ID()
			dep, derr := c.GetRuleExclusionDeployment(ctx, id)
			if derr != nil {
				addWarning("rule_exclusions/"+id, derr)
				continue
			}
			if dep.Archived {
				continue
			}
			target := fmt.Sprintf("%s (%s)", ex.DisplayName, id)
			items = append(items, smokeCleanupItem{
				Surface: "rule_exclusions",
				Action:  "archive",
				Target:  target,
				apply: func() error {
					_, err := c.UpdateRuleExclusionDeployment(ctx, id, chronicle.RuleExclusionDeploymentUpdate{
						Enabled:  new(false),
						Archived: new(true),
					})
					return err
				},
			})
		}
	}

	return items, warnings
}

func applySmokeCleanup(items []smokeCleanupItem) error {
	for _, it := range items {
		if it.apply == nil {
			continue
		}
		if err := it.apply(); err != nil {
			return fmt.Errorf("%s %s %s: %w", it.Surface, it.Action, it.Target, err)
		}
	}
	return nil
}

func isSecopsctlSmokeName(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{
		"secopsctl-smoketest-",
		"secopsctl_smoketest_",
		"secopsctl-smoke-",
		"secopsctl_smoke_",
	} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
