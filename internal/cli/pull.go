package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
)

// pullTarget binds a CLI target name to its on-disk subdirectory (relative to
// the data root) and the mirror puller that writes it. filter is honored only
// by the curated_rules puller; all other pullers ignore it.
type pullTarget struct {
	name   string
	subdir string
	run    func(c *chronicle.Client, outDir, filter string) (int, error)
}

// pullOrder is the canonical target order, also used to expand "all".
var pullOrder = []pullTarget{
	{"rules", mirror.DirRules, func(c *chronicle.Client, out, _ string) (int, error) {
		return mirror.PullRules(baseContext(), c, out)
	}},
	{"reference_lists", mirror.DirRefLists, func(c *chronicle.Client, out, _ string) (int, error) {
		return mirror.PullReferenceLists(baseContext(), c, out)
	}},
	{"data_tables", mirror.DirDataTables, func(c *chronicle.Client, out, _ string) (int, error) {
		return mirror.PullDataTables(baseContext(), c, out)
	}},
	{"dashboards", mirror.DirDashboards, func(c *chronicle.Client, out, _ string) (int, error) {
		// dashboards reconcile on the engine: pull the canonical config (CUSTOM
		// only) so a pulled snapshot pushes back via `push dashboards`. (The legacy
		// export-envelope puller is not round-trippable through the engine.)
		s, ok := mirror.BuildSIEMSurface("dashboards", c)
		if !ok {
			return 0, fmt.Errorf("dashboards surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"curated", mirror.DirCurated, func(c *chronicle.Client, out, _ string) (int, error) {
		return mirror.PullCurated(baseContext(), c, out)
	}},
	{"curated_rules", filepath.Join(mirror.DirCurated, "rules"), func(c *chronicle.Client, out, filter string) (int, error) {
		return mirror.PullCuratedRules(baseContext(), c, out, filter)
	}},
	{"feeds", mirror.DirFeeds, func(c *chronicle.Client, out, _ string) (int, error) {
		return mirror.PullFeeds(baseContext(), c, out)
	}},
	{"parsers", mirror.DirParsers, func(c *chronicle.Client, out, _ string) (int, error) {
		// nil log types: derive the active set from configured feeds.
		return mirror.PullParsers(baseContext(), c, out, nil)
	}},
	{"rule_exclusions", mirror.DirRuleExcl, func(c *chronicle.Client, out, _ string) (int, error) {
		// No legacy puller — pull through the engine surface.
		s, ok := mirror.BuildSIEMSurface("rule_exclusions", c)
		if !ok {
			return 0, fmt.Errorf("rule_exclusions surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"metric_definitions", mirror.DirMetrics, func(c *chronicle.Client, out, _ string) (int, error) {
		// No legacy puller — pull through the engine surface.
		s, ok := mirror.BuildSIEMSurface("metric_definitions", c)
		if !ok {
			return 0, fmt.Errorf("metric_definitions surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"scheduled_reports", mirror.DirScheduledReports, func(c *chronicle.Client, out, _ string) (int, error) {
		// No legacy puller — pull through the engine surface.
		s, ok := mirror.BuildSIEMSurface("scheduled_reports", c)
		if !ok {
			return 0, fmt.Errorf("scheduled_reports surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"datataps", mirror.DirDataTaps, func(c *chronicle.Client, out, _ string) (int, error) {
		s, ok := mirror.BuildSIEMSurface("datataps", c)
		if !ok {
			return 0, fmt.Errorf("datataps surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"error_notifications", mirror.DirErrorNotifs, func(c *chronicle.Client, out, _ string) (int, error) {
		s, ok := mirror.BuildSIEMSurface("error_notifications", c)
		if !ok {
			return 0, fmt.Errorf("error_notifications surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"federation_groups", mirror.DirFederation, func(c *chronicle.Client, out, _ string) (int, error) {
		s, ok := mirror.BuildSIEMSurface("federation_groups", c)
		if !ok {
			return 0, fmt.Errorf("federation_groups surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
	{"forwarders", mirror.DirForwarders, func(c *chronicle.Client, out, _ string) (int, error) {
		// Symmetry with `push forwarders` / `drift forwarders`: pull the engine
		// surface so `pull all` mirrors forwarders too (otherwise drift flags them).
		s, ok := mirror.BuildSIEMSurface("forwarders", c)
		if !ok {
			return 0, fmt.Errorf("forwarders surface not registered")
		}
		return reconcile.Pull(baseContext(), s, out, os.Stdout)
	}},
}

// targetByName indexes pullOrder for the explicit single-target dispatch.
func targetByName() map[string]pullTarget {
	m := make(map[string]pullTarget, len(pullOrder))
	for _, t := range pullOrder {
		m[t.name] = t
	}
	return m
}

func init() {
	var (
		filterExpr string
		outDir     string
	)

	byName := targetByName()
	names := make([]string, 0, len(pullOrder)+1)
	for _, t := range pullOrder {
		names = append(names, t.name)
	}
	names = append(names, "all")

	pullCmd := &cobra.Command{
		Use:   "pull <target>",
		Short: "Snapshot live SecOps state to local files (read-only)",
		Long: "Pull mirrors live Chronicle state into local files for review under\n" +
			"git. It never mutates the instance.\n\n" +
			"Targets: " + strings.Join(names, ", ") + "\n" +
			"'all' pulls every target in order. --filter applies only to curated_rules.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: names,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			// Resolve the ordered set of targets to run.
			var todo []pullTarget
			if target == "all" {
				todo = pullOrder
			} else {
				t, ok := byName[target]
				if !ok {
					return fmt.Errorf("unknown pull target %q (want one of: %s)",
						target, strings.Join(names, ", "))
				}
				todo = []pullTarget{t}
			}

			// --filter is only meaningful for curated_rules. Warn (don't fail)
			// when it is set for any other single target.
			if filterExpr != "" && target != "curated_rules" && target != "all" {
				fmt.Fprintf(os.Stderr,
					"warning: --filter only applies to 'curated_rules'; ignored for %q\n",
					target)
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}

			root := mirror.DataRoot(outDir)

			total := 0
			for _, t := range todo {
				dest := filepath.Join(root, t.subdir)
				n, err := t.run(c, dest, filterExpr)
				if err != nil {
					return fmt.Errorf("pull %s: %w", t.name, err)
				}
				total += n
			}

			fmt.Printf("\nPull complete: %d item(s) across %d target(s).\n",
				total, len(todo))
			return nil
		},
	}

	pf := pullCmd.Flags()
	pf.StringVar(&filterExpr, "filter", "",
		"filter expression (only used by 'curated_rules')")
	pf.StringVar(&outDir, "out", "",
		"output root directory for pulled artifacts (default: cwd)")
	// `pull <target> --help` appends the target's behavior note.
	attachTargetHelp(pullCmd, names)

	rootCmd.AddCommand(pullCmd)
}
