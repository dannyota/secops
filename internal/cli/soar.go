package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// soarSettings maps the loaded instance config to SOAR settings (the tenant SOAR
// host plus the v1alpha path components). soar_url is stored already-canonical
// (see normalizeSOARURL at save time), so it is used as-is here.
func soarSettings(inst *config.Instance) soar.Settings {
	cs := inst.Settings()
	return soar.Settings{
		BaseURL:       inst.SOARURL,
		ProjectNumber: cs.ProjectNumber,
		Region:        cs.Region,
		CustomerID:    cs.CustomerID,
		ForceIPv4:     inst.ForceIPv4,
	}
}

// normalizeSOARURL tolerates a bare host in soar_url: SOAR is always HTTPS, so a
// value with no scheme gets "https://" prepended. A trailing slash is trimmed so
// the transport doesn't build "//"-joined paths. Applied once, when `config`
// saves the file — not at run time.
func normalizeSOARURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return strings.TrimRight(s, "/")
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
	soarCmd.AddCommand(newSOARPullCmd(), newSOARPushCmd(), newSOARLegacyCmd())
	rootCmd.AddCommand(soarCmd)
}

func newSOARPullCmd() *cobra.Command {
	var out string
	bespoke := []string{"connectors", "jobs", "grouping", "cases", "playbooks"}
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

			conn := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARConnectors(ctx, c, d) }
			jobs := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARJobs(ctx, c, d) }
			grp := func(c *soar.Client, d string) (int, error) { return mirror.PullSOARGrouping(ctx, c, d) }
			cases := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARCases(ctx, lc, d) }
			plays := func(lc *legacy.Client, d string) (int, error) { return mirror.PullSOARPlaybooks(ctx, lc, d) }

			if slices.Contains(engine, target) {
				return runEngine(target)
			}
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
				if err := runLegacy(plays, mirror.DirSOARPlaybooks); err != nil {
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
		Long: "Reconcile local files to live SOAR config. Engine surfaces (" +
			strings.Join(mirror.SOARSurfaceNames(), ", ") + ") create new files,\n" +
			"update edited ones, and (only with --prune) delete server-only objects.",
	}
	cmd.AddCommand(
		newSOARBulkCloseCmd(),
		newSOARPatchCmd("connector", "patch a connector instance from an edited snapshot YAML"),
		newSOARPatchCmd("job", "patch a job instance from an edited snapshot YAML"),
		newSOARPlaybookSaveCmd(),
	)
	for _, name := range mirror.SOARSurfaceNames() {
		cmd.AddCommand(newSOAREnginePushCmd(name))
	}
	return cmd
}

// newSOAREnginePushCmd builds the guarded reconcile push for one engine surface:
// `soar push <surface> [--dry-run|--yes] [--prune]`.
func newSOAREnginePushCmd(name string) *cobra.Command {
	var (
		dryRun bool
		yes    bool
		prune  bool
		out    string
	)
	cmd := &cobra.Command{
		Use:   name + " [--prune]",
		Short: "Reconcile local " + name + " files to live (create/update; --prune to delete)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			s, ok := mirror.BuildSOARSurface(name, lc)
			if !ok {
				return fmt.Errorf("unknown engine surface %q", name)
			}
			dr, ay := soarGuard(name+" reconcile", dryRun, yes)
			dir := filepath.Join(mirror.DataRoot(out), mirror.DirSOAR, s.Dir)
			_, err = reconcile.Push(baseContext(), s, dir, reconcile.PushOpts{
				DryRun: dr, AssumeYes: ay, Prune: prune,
			}, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	f.BoolVar(&prune, "prune", false, "also delete live objects with no local file (guarded; gated on a complete pull)")
	f.StringVar(&out, "out", "", "data root directory (default: cwd)")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
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

// newSOARLegacyCmd is the raw escape hatch for external-API operations not yet
// modeled as engine surfaces — so the full Siemplify surface is reachable as
// config-as-code (GET/POST-read to pull JSON, a guarded mutating method to push
// it back) without a typed wrapper per endpoint.
func newSOARLegacyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "legacy",
		Short: "Escape hatch: call any Siemplify external-API op (/api/external/v1)",
	}
	cmd.AddCommand(newSOARLegacyCallCmd())
	return cmd
}

func newSOARLegacyCallCmd() *cobra.Command {
	var (
		method string
		body   string
		write  bool
		yes    bool
		out    string
	)
	cmd := &cobra.Command{
		Use:   "call <op>",
		Short: "Call an external-API op, e.g. integrations/GetInstalledIntegrations",
		Long: "op is the path under /api/external/v1 (leading slash optional). GET and\n" +
			"POST default to read-only; PUT/DELETE or --write mark a mutation, which\n" +
			"prints the LIVE banner and requires --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op := args[0]
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				method = "GET"
			}

			var payload any
			if body != "" {
				raw, err := os.ReadFile(body)
				if err != nil {
					return err
				}
				if !json.Valid(raw) {
					return fmt.Errorf("%s is not valid JSON", body)
				}
				payload = json.RawMessage(raw)
			}

			if write || method == "PUT" || method == "DELETE" {
				legacyCallBanner(method, op)
				if !yes {
					fmt.Fprintln(os.Stdout, "Refusing a mutating call without --yes. Aborted.")
					return nil
				}
			}

			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			resp, err := lc.Raw(baseContext(), method, op, payload)
			if err != nil {
				return err
			}
			pretty := indentJSON(resp)
			if out != "" {
				if err := os.WriteFile(out, pretty, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "wrote %d bytes -> %s\n", len(pretty), out)
				return nil
			}
			_, err = os.Stdout.Write(pretty)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&method, "method", "GET", "HTTP method (GET, POST, PUT, DELETE)")
	f.StringVar(&body, "body", "", "JSON file to send as the request body")
	f.BoolVar(&write, "write", false, "mark this call as mutating (forces the guard for a POST that writes)")
	f.BoolVar(&yes, "yes", false, "confirm a mutating call")
	f.StringVar(&out, "out", "", "write the response to this file instead of stdout")
	return cmd
}

// legacyCallBanner warns before a mutating raw external-API call.
func legacyCallBanner(method, op string) {
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(os.Stdout, bar)
	fmt.Fprintln(os.Stdout, "!! LIVE external-API call to a PRODUCTION SOAR tenant !!")
	fmt.Fprintf(os.Stdout, "!! %s %s\n", method, op)
	fmt.Fprintln(os.Stdout, bar)
}

// indentJSON pretty-prints a raw response for stdout/file output.
func indentJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return append([]byte(nil), raw...)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
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
