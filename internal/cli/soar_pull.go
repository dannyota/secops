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
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

func newSOARPullCmd() *cobra.Command {
	var out string
	bespoke := []string{"grouping", "cases"}
	engine := mirror.SOARSurfaceNames()
	valid := append(append(append([]string{}, bespoke...), engine...), "all")

	cmd := &cobra.Command{
		Use:   "pull <target>",
		Short: "Read-only: snapshot SOAR state to local files",
		Long: "Targets: " + strings.Join(valid, ", ") + ".\n" +
			"Engine surfaces (" + strings.Join(engine, ", ") + ") snapshot one\n" +
			"redacted, diff-friendly file per object for the pull->diff->push loop.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: valid,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			root := filepath.Join(mirror.DataRoot(out), mirror.DirSOAR)
			ctx := baseContext()

			runModern := func(fn func(*soar.Client, string) (int, error), sub string) error {
				c, err := newSOARClient()
				if err != nil {
					return err
				}
				_, err = fn(c, filepath.Join(root, sub))
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
				_, err = reconcile.Pull(ctx, s, filepath.Join(root, s.Dir), os.Stdout)
				return err
			}

			grp := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARGrouping(ctx, c, d) }
			cases := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARCases(ctx, lc, d) }

			if slices.Contains(engine, target) {
				return runEngine(target)
			}
			switch target {
			case "grouping":
				return runModern(grp, mirror.DirSOARGrouping)
			case "cases":
				return runLegacy(cases, mirror.DirSOARCases)
			case "all":
				if err := runModern(grp, mirror.DirSOARGrouping); err != nil {
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
	cmd.Flags().StringVar(&out, "out", "", "output root directory (default: cwd)")
	return cmd
}
