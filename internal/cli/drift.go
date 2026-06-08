package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
)

// newDriftCmd builds the read-only drift gate: it compares the committed local
// files to live state across the reconcile surfaces and reports divergence,
// exiting non-zero when any surface has drifted. Intended for CI (pull → commit →
// drift). It never mutates the tenant.
func newDriftCmd() *cobra.Command {
	var (
		out      string
		siemOnly bool
		soarOnly bool
	)
	siem := mirror.SIEMSurfaceNames()
	soar := mirror.SOARSurfaceNames()
	all := append(append([]string{}, siem...), soar...)

	cmd := &cobra.Command{
		Use:   "drift [target...]",
		Short: "Read-only: report how live state has drifted from local files (CI gate)",
		Long: "Compare committed local files to live state across the reconcile surfaces\n" +
			"and report divergence (local-only +, changed ~, live-only -). Never mutates.\n" +
			"Exit codes (git-style): 0 in sync · 2 drift detected (act) · 1 error.\n" +
			"Run it after `pull` in CI.\n\n" +
			"With no target, checks every engine surface; otherwise the named ones.\n" +
			"Targets: " + strings.Join(all, ", "),
		ValidArgs: all,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := baseContext()
			root := mirror.DataRoot(out)

			wantSIEM, wantSOAR, unknown := selectDriftTargets(args, siem, soar)
			if len(unknown) > 0 {
				// Never silently skip a requested target — a typo'd surface would
				// otherwise let the gate pass without checking what the caller meant.
				return fmt.Errorf("drift: unknown target(s): %s\nvalid: %s",
					strings.Join(unknown, ", "), strings.Join(all, ", "))
			}
			// Plane selectors scope a no-arg run to one plane so a single-plane CI
			// runner needs only that plane's credentials.
			if siemOnly {
				wantSOAR = nil
			}
			if soarOnly {
				wantSIEM = nil
			}

			var targets []mirror.DriftTarget
			if len(wantSIEM) > 0 {
				c, err := newChronicleClient()
				if err != nil {
					return fmt.Errorf("drift: build SIEM client: %w", err)
				}
				for _, name := range wantSIEM {
					s, ok := mirror.BuildSIEMSurface(name, c)
					if !ok {
						return fmt.Errorf("drift: SIEM surface %q not registered", name)
					}
					targets = append(targets, mirror.DriftTarget{Surface: s, Dir: filepath.Join(root, s.Dir)})
				}
			}
			if len(wantSOAR) > 0 {
				lc, err := newSOARLegacyClient()
				if err != nil {
					return fmt.Errorf("drift: build SOAR client: %w", err)
				}
				for _, name := range wantSOAR {
					s, ok := mirror.BuildSOARSurface(name, lc)
					if !ok {
						return fmt.Errorf("drift: SOAR surface %q not registered", name)
					}
					targets = append(targets, mirror.DriftTarget{Surface: s, Dir: filepath.Join(root, mirror.DirSOAR, s.Dir)})
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("drift: no matching reconcile surfaces")
			}

			var w io.Writer = os.Stdout
			if jsonOut {
				w = io.Discard // suppress the human report; emit JSON below
			} else {
				fmt.Fprintf(os.Stdout, "Drift check (%d surface(s)) — local vs live:\n", len(targets))
			}
			rep := mirror.Drift(ctx, targets, w)

			drifted, indet := 0, 0
			for _, it := range rep.Items {
				switch {
				case it.Drifted():
					drifted++
				case it.Indeterminate():
					indet++
				}
			}

			if jsonOut {
				type surf struct {
					Name       string `json:"name"`
					Created    int    `json:"created"`
					Updated    int    `json:"updated"`
					Deleted    int    `json:"deleted"`
					Untracked  int    `json:"untracked"`
					Incomplete bool   `json:"incomplete"`
					Drifted    bool   `json:"drifted"`
					Error      string `json:"error,omitempty"`
				}
				out := struct {
					DriftedSurfaces       int    `json:"drifted_surfaces"`
					IndeterminateSurfaces int    `json:"indeterminate_surfaces"`
					Surfaces              []surf `json:"surfaces"`
				}{DriftedSurfaces: drifted, IndeterminateSurfaces: indet}
				for _, it := range rep.Items {
					s := surf{
						Name: it.Name, Created: it.Created, Updated: it.Updated,
						Deleted: it.Deleted, Untracked: it.Untracked,
						Incomplete: it.Incomplete, Drifted: it.Drifted(),
					}
					if it.Err != nil {
						s.Error = it.Err.Error()
					}
					out.Surfaces = append(out.Surfaces, s)
				}
				if err := emitJSON(out); err != nil {
					return err
				}
			}

			// Exit codes (git-style): drift present → 2 (divergence, "act");
			// indeterminate-only → 1 (error, "retry/fix"); clean → 0.
			switch {
			case drifted > 0 && indet > 0:
				return divergence("drift detected in %d surface(s); %d could not be verified — review, then `pull`/`push`", drifted, indet)
			case drifted > 0:
				return divergence("drift detected in %d surface(s) — review and `pull`/`push` to reconcile", drifted)
			case indet > 0:
				return fmt.Errorf("could not verify %d surface(s) (live list incomplete) — re-run", indet)
			}
			if !jsonOut {
				fmt.Fprintln(os.Stdout, "\nNo drift — local matches live.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "data root directory (default: cwd)")
	cmd.Flags().BoolVar(&siemOnly, "siem", false, "check only SIEM surfaces (needs only ADC creds)")
	cmd.Flags().BoolVar(&soarOnly, "soar", false, "check only SOAR surfaces (needs only the AppKey)")
	cmd.MarkFlagsMutuallyExclusive("siem", "soar")
	// `drift <target> --help` appends the target's behavior note.
	attachTargetHelp(cmd, all)
	return cmd
}

// selectDriftTargets splits the requested targets into SIEM and SOAR sets. No args
// means "all" on both planes; otherwise each arg is matched to its plane, and any
// arg matching neither is returned in `unknown` so the caller can refuse it (never
// silently skip a requested target).
func selectDriftTargets(args, siem, soar []string) (wantSIEM, wantSOAR, unknown []string) {
	if len(args) == 0 {
		return siem, soar, nil
	}
	for _, a := range args {
		switch {
		case slices.Contains(siem, a):
			wantSIEM = append(wantSIEM, a)
		case slices.Contains(soar, a):
			wantSOAR = append(wantSOAR, a)
		default:
			unknown = append(unknown, a)
		}
	}
	return wantSIEM, wantSOAR, unknown
}

func init() { rootCmd.AddCommand(newDriftCmd()) }
