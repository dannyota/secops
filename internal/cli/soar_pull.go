package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

func newSOARPullCmd() *cobra.Command {
	var (
		out     string
		prune   bool
		redacts []string
	)
	bespoke := []string{"grouping", "cases"}
	engine := mirror.SOARSurfaceNames()
	valid := append(append(append([]string{}, bespoke...), engine...), "all")

	cmd := &cobra.Command{
		Use:   "pull <target>",
		Short: "Read-only: snapshot SOAR state to local files",
		Long: "Targets: " + strings.Join(valid, ", ") + ".\n" +
			"Engine surfaces (" + strings.Join(engine, ", ") + ") snapshot one\n" +
			"redacted, diff-friendly file per object for the pull->diff->push loop.\n\n" +
			"--prune removes local files whose live counterparts have been deleted,\n" +
			"so the mirror directory is an exact reflection of the instance. Refused\n" +
			"on an incomplete listing to prevent false deletions.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: valid,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			dataRoot := mirror.DataRoot(out)
			root := filepath.Join(dataRoot, mirror.DirSOAR)
			ctx := baseContext()
			pullOpts := reconcile.PullOpts{Prune: prune}
			// Mask inline-secret values (e.g. a webhook URL with a token in a
			// playbook step param) per .secopsctl-redact + --redact before writing.
			if err := applyValueRedaction(dataRoot, redacts); err != nil {
				return err
			}

			runLegacy := func(fn func(*legacy.Client, string) (int, error), sub string) error {
				lc, err := newSOARLegacyClient()
				if err != nil {
					return err
				}
				_, err = fn(lc, filepath.Join(root, sub))
				return err
			}
			runEngine := func(name string) error {
				lc, err := newSOARLegacyClient()
				if err != nil {
					return err
				}
				s, ok := mirror.BuildSOARSurface(name, lc)
				if !ok {
					return fmt.Errorf("unknown engine surface %q", name)
				}
				_, err = reconcile.Pull(ctx, s, filepath.Join(root, s.Dir), os.Stdout, pullOpts)
				return err
			}

			// Grouping rules use the MODERN soar client; the settings singleton is
			// read from the reliable legacy AppKey config endpoint — so it needs both.
			grp := func(d string) error {
				c, err := newSOARClient()
				if err != nil {
					return err
				}
				lc, err := newSOARLegacyClient()
				if err != nil {
					return err
				}
				_, err = mirror.PullSOARGrouping(ctx, c, lc, d, prune)
				return err
			}
			cases := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARCases(ctx, lc, d) }

			if slices.Contains(engine, target) {
				return runEngine(target)
			}
			switch target {
			case "grouping":
				return grp(filepath.Join(root, mirror.DirSOARGrouping))
			case "cases":
				return runLegacy(cases, mirror.DirSOARCases)
			case "all":
				if err := grp(filepath.Join(root, mirror.DirSOARGrouping)); err != nil {
					return err
				}
				if err := runLegacy(cases, mirror.DirSOARCases); err != nil {
					return err
				}
				for _, n := range engine {
					if err := runEngine(n); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unknown soar pull target %q", target)
			}
		},
	}
	f := cmd.Flags()
	f.StringVar(&out, "out", "", "output root directory (default: cwd)")
	f.BoolVar(&prune, "prune", false, "remove local files with no live counterpart")
	f.StringArrayVar(&redacts, "redact", nil, "regex masking matched substrings of inline string values on write (repeatable); for a durable, drift-safe rule put the pattern in .secopsctl-redact at the data root")
	return cmd
}
