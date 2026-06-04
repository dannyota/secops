package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// soarSettings maps the loaded instance config to SOAR settings (the tenant SOAR
// host plus the v1alpha path components).
func soarSettings(inst *config.Instance) soar.Settings {
	cs := inst.Settings()
	return soar.Settings{
		BaseURL:       inst.SOARURL,
		ProjectNumber: cs.ProjectNumber,
		Region:        cs.Region,
		CustomerID:    cs.CustomerID,
	}
}

// soarAppKey resolves the SOAR AppKey (no ADC) from the resolved config — which
// already reflects the SECOPS_SOAR_APP_KEY env override — falling back to the
// legacy SECOPS_API_KEY env var.
func soarAppKey(inst *config.Instance) (string, error) {
	if inst.SOARAppKey != "" {
		return inst.SOARAppKey, nil
	}
	if key := auth.FromEnv("SECOPS_API_KEY"); key != "" {
		return key, nil
	}
	return "", fmt.Errorf("SOAR AppKey not set; run `secopsctl config` or export SECOPS_SOAR_APP_KEY")
}

func newSOARSettings() (soar.Settings, string, error) {
	inst, err := loadInstance()
	if err != nil {
		return soar.Settings{}, "", err
	}
	s := soarSettings(inst)
	if s.BaseURL == "" {
		return soar.Settings{}, "", fmt.Errorf("soar_url is not set in the instance config (the tenant SOAR host)")
	}
	key, err := soarAppKey(inst)
	if err != nil {
		return soar.Settings{}, "", err
	}
	return s, key, nil
}

func newSOARClient() (*soar.Client, error) {
	s, key, err := newSOARSettings()
	if err != nil {
		return nil, err
	}
	return soar.NewClient(s, auth.SOARAppKey(key))
}

func newSOARLegacyClient() (*legacy.Client, error) {
	s, key, err := newSOARSettings()
	if err != nil {
		return nil, err
	}
	return legacy.NewClient(s, auth.SOARAppKey(key), nil), nil
}

func init() {
	soarCmd := &cobra.Command{
		Use:   "soar",
		Short: "Operate Google SecOps SOAR (Siemplify) as code (AppKey auth, no ADC)",
		Long: "Operate the SOAR surface: read-only `pull` of connectors, jobs, grouping\n" +
			"rules, cases, and playbooks, and guarded mutating `push`. SOAR uses a\n" +
			"long-lived AppKey ($SECOPS_SOAR_APP_KEY) and the soar_url config host.",
	}
	soarCmd.AddCommand(newSOARPullCmd(), newSOARPushCmd())
	rootCmd.AddCommand(soarCmd)
}

func newSOARPullCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:       "pull <target>",
		Short:     "Read-only: snapshot SOAR state to local files",
		Long:      "Targets: connectors, jobs, grouping, cases, playbooks, all.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"connectors", "jobs", "grouping", "cases", "playbooks", "all"},
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

			conn := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARConnectors(ctx, c, d) }
			jobs := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARJobs(ctx, c, d) }
			grp := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARGrouping(ctx, c, d) }
			cases := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARCases(ctx, lc, d) }
			plays := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARPlaybooks(ctx, lc, d) }

			switch target {
			case "connectors":
				return runModern(conn, mirror.DirSOARConnectors)
			case "jobs":
				return runModern(jobs, mirror.DirSOARJobs)
			case "grouping":
				return runModern(grp, mirror.DirSOARGrouping)
			case "cases":
				return runLegacy(cases, mirror.DirSOARCases)
			case "playbooks":
				return runLegacy(plays, mirror.DirSOARPlaybooks)
			case "all":
				if err := runModern(conn, mirror.DirSOARConnectors); err != nil {
					return err
				}
				if err := runModern(jobs, mirror.DirSOARJobs); err != nil {
					return err
				}
				if err := runModern(grp, mirror.DirSOARGrouping); err != nil {
					return err
				}
				if err := runLegacy(cases, mirror.DirSOARCases); err != nil {
					return err
				}
				return runLegacy(plays, mirror.DirSOARPlaybooks)
			default:
				return fmt.Errorf("unknown soar pull target %q", target)
			}
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output root directory (default: cwd)")
	return cmd
}

// soarGuard derives the dry-run / confirmation state from the standard flags and
// the interactive prompt, mirroring `push`.
func soarGuard(target string, dryRunFlag, yesFlag bool) (dryRun, assumeYes bool) {
	dryRun = dryRunFlag || !yesFlag
	assumeYes = yesFlag && !dryRunFlag
	if !dryRun && !assumeYes && confirmPush(target) {
		assumeYes = true
	}
	return dryRun, assumeYes
}

func newSOARPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push <target>",
		Short: "MUTATING (guarded): live SOAR changes. Dry-run by default",
	}
	cmd.AddCommand(
		newSOARBulkCloseCmd(),
		newSOARPatchCmd("connector", "patch a connector instance from an edited snapshot YAML"),
		newSOARPatchCmd("job", "patch a job instance from an edited snapshot YAML"),
		newSOARPlaybookSaveCmd(),
	)
	return cmd
}

func newSOARBulkCloseCmd() *cobra.Command {
	var (
		idsArg    string
		reason    int
		rootCause string
		comment   string
		dryRun    bool
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "bulk-close",
		Short: "Bulk-close SOAR cases by id (reason 0=NotMalicious 1=Malicious 2=Maintenance 3=Inconclusive)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseIntList(idsArg)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("bulk-close cases", dryRun, yes)
			_, err = mirror.PushSOARBulkClose(baseContext(), lc, ids, legacy.CloseReason(reason), rootCause, comment, dr, ay, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&idsArg, "ids", "", "comma-separated SOAR case ids (required)")
	f.IntVar(&reason, "reason", 0, "close reason: 0 NotMalicious, 1 Malicious, 2 Maintenance, 3 Inconclusive")
	f.StringVar(&rootCause, "root-cause", "", "close root cause")
	f.StringVar(&comment, "comment", "", "close comment")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("ids")
	return cmd
}

func newSOARPatchCmd(kind, short string) *cobra.Command {
	var (
		file   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   kind + " --file <snapshot.yaml>",
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard(kind+" patch", dryRun, yes)
			if kind == "connector" {
				return mirror.PushSOARConnectorPatch(baseContext(), c, file, dr, ay, os.Stdout)
			}
			return mirror.PushSOARJobPatch(baseContext(), c, file, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "edited snapshot YAML to apply (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newSOARPlaybookSaveCmd() *cobra.Command {
	var (
		file   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "playbook --file <playbook.json>",
		Short: "Save a playbook definition (whole-body replace; mints a new version)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("playbook save", dryRun, yes)
			return mirror.PushSOARPlaybookSave(baseContext(), lc, file, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "playbook JSON to save (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// parseIntList parses "1,2,3" into []int.
func parseIntList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("no ids given")
	}
	var out []int
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", part, err)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid ids parsed from %q", s)
	}
	return out, nil
}
