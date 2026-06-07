package cli

import (
	"bytes"
	"context"
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
// host plus the v1alpha path components). `config` normalizes soar_url at save
// time, but a value from $SECOPS_SOAR_URL or a hand-edited file skips that, so
// normalize again here (cheap, idempotent) to tolerate a bare host / trailing slash.
func soarSettings(inst *config.Instance) soar.Settings {
	cs := inst.Settings()
	return soar.Settings{
		BaseURL:       normalizeSOARURL(inst.SOARURL),
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
	soarCmd.AddCommand(newSOARPullCmd(), newSOARPushCmd(), newSOARCaseCmd(), newSOARLegacyCmd(),
		newSOARIntegrationCmd(), newSOARSettingsCmd(), newSOARMarketplaceCmd())
	rootCmd.AddCommand(soarCmd)
}

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
		reason    string
		rootCause string
		comment   string
		dryRun    bool
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "bulk-close",
		Short: "Bulk-close SOAR cases by id (reason: malicious|not-malicious|maintenance|inconclusive|unknown)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseIntList(idsArg)
			if err != nil {
				return err
			}
			cr, err := parseCloseReason(reason)
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("bulk-close cases", dryRun, yes)
			_, err = mirror.PushSOARBulkClose(baseContext(), lc, ids, cr, rootCause, comment, dr, ay, os.Stdout)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&idsArg, "ids", "", "comma-separated SOAR case ids (required)")
	f.StringVar(&reason, "reason", "maintenance", "close reason: malicious | not-malicious | maintenance | inconclusive | unknown")
	f.StringVar(&rootCause, "root-cause", "", "close root cause")
	f.StringVar(&comment, "comment", "", "close comment")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("ids")
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
		method   string
		body     string
		write    bool
		readOnly bool
		yes      bool
		out      string
	)
	cmd := &cobra.Command{
		Use:   "call <op>",
		Short: "Call an external-API op, e.g. integrations/GetInstalledIntegrations",
		Long: "op is the path under /api/external/v1 (leading slash optional). GET is\n" +
			"read-only. The legacy API uses POST for BOTH reads and writes, so a POST\n" +
			"must declare intent: --read for a read-only call, or --write for a mutation.\n" +
			"PUT/DELETE/--write print the LIVE banner and require --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op := args[0]
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				method = "GET"
			}
			// (--read and --write are mutually exclusive — enforced by cobra below.)

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

			// A POST can read or write on this API, so it is fail-closed: without an
			// explicit --read or --write it is refused rather than run ungated (a
			// forgotten --write on a write-POST would otherwise deploy live silently).
			if method == "POST" && !write && !readOnly {
				return fmt.Errorf("POST on the legacy API can read OR write; pass --read for a read-only call or --write (with --yes) for a mutation")
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
	f.BoolVar(&write, "write", false, "mark this call as mutating (LIVE banner + --yes); required for a write-POST")
	f.BoolVar(&readOnly, "read", false, "assert a read-only POST (skips the mutation guard)")
	f.BoolVar(&yes, "yes", false, "confirm a mutating call")
	f.StringVar(&out, "out", "", "write the response to this file instead of stdout")
	cmd.MarkFlagsMutuallyExclusive("read", "write")
	return cmd
}

// newSOARIntegrationCmd groups the imperative integration-instance verbs.
// Integration instances are not reconcilable (no update endpoint, no round-tripping
// read shape), so they are operated imperatively; reads stay on `soar legacy call`.
func newSOARIntegrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Manage SOAR integration instances (imperative create/delete)",
	}
	cmd.AddCommand(newSOARIntegrationCreateCmd(), newSOARIntegrationDeleteCmd(),
		newSOARIntegrationListCmd(), newSOARIntegrationUninstallCmd(),
		newSOARIntegrationConnectorCmd())
	return cmd
}

// newSOARIntegrationConnectorCmd groups the connector-DEFINITION verbs (the
// connector templates inside an integration, as opposed to the configured
// connector instances under `soar pull/push connectors`).
func newSOARIntegrationConnectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connector",
		Short: "List/delete connector definitions inside an integration",
	}
	cmd.AddCommand(newSOARConnectorDefListCmd(), newSOARConnectorDefDeleteCmd())
	return cmd
}

func newSOARConnectorDefListCmd() *cobra.Command {
	var (
		integration string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "list --integration <key>",
		Short: "List an integration's connector definitions (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			defs, err := c.ListConnectors(baseContext(), integration)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(defs)
			}
			for _, d := range defs {
				tag := ""
				if d.Custom {
					tag = "  [custom/deletable]"
				}
				fmt.Fprintf(os.Stdout, "%-6s %s%s\n", d.ID.String(), d.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d connector definition(s)\n", len(defs))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier (required)")
	f.BoolVar(&asJSON, "json", false, "emit JSON")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func newSOARConnectorDefDeleteCmd() *cobra.Command {
	var (
		integration string
		id          string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration <key> --id <connector-id>",
		Short: "Delete a custom connector definition (e.g. a 'Copy of …' duplicate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			def, err := c.GetConnectorDef(ctx, integration, id)
			if err != nil {
				return fmt.Errorf("connector definition %s/%s not found: %w", integration, id, err)
			}
			if !def.Custom {
				return fmt.Errorf("connector %q (id %s) is a commercial definition, not deletable", def.DisplayName, id)
			}
			dr, _ := soarGuard("integration connector delete", dryRun, yes)
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN: would delete custom connector definition %q (%s/%s)\n", def.DisplayName, integration, id)
				return nil
			}
			if err := c.DeleteConnectorDef(ctx, integration, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted custom connector definition %q (%s/%s)\n", def.DisplayName, integration, id)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier (required)")
	f.StringVar(&id, "id", "", "numeric connector-definition id (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

// newSOARIntegrationListCmd lists installed integration packs via the modern
// v1alpha surface — the discovery side of uninstall. Read-only.
func newSOARIntegrationListCmd() *cobra.Command {
	var (
		asJSON bool
		custom bool
	)
	cmd := &cobra.Command{
		Use:   "list [--custom] [--json]",
		Short: "List installed integration packs (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ints, err := c.ListIntegrations(baseContext())
			if err != nil {
				return err
			}
			if custom {
				ints = slices.DeleteFunc(ints, func(i soar.Integration) bool { return !soar.IsDeletableIntegration(i) })
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(ints)
			}
			for _, i := range ints {
				tag := ""
				if soar.IsDeletableIntegration(i) {
					tag = "  [deletable]"
				}
				fmt.Fprintf(os.Stdout, "%-52s %s%s\n", i.Identifier, i.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d integration(s)\n", len(ints))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&custom, "custom", false, "show only deletable (custom pack or clone) integrations")
	f.BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// newSOARIntegrationUninstallCmd deletes a CUSTOM integration pack (e.g. a cloned
// "Copy of …") by its addressable key via the v1alpha integrations.delete path.
// Commercial/marketplace packs are not deletable. Guarded LIVE MUTATION.
func newSOARIntegrationUninstallCmd() *cobra.Command {
	var (
		name   string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall --name <integration-key>",
		Short: "Delete a custom integration pack (clone) by its key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			target, err := resolveCustomIntegration(ctx, c, name)
			if err != nil {
				return err
			}
			dr, _ := soarGuard("integration uninstall", dryRun, yes)
			key := target.Name
			if key == "" {
				key = target.Identifier
			}
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN: would delete custom integration %q (%s)\n", target.DisplayName, key)
				return nil
			}
			if err := c.DeleteIntegration(ctx, key); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted custom integration %q (%s)\n", target.DisplayName, key)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "integration key: Name (clone), Identifier, or displayName (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// resolveCustomIntegration finds the integration addressed by key (matched against
// Name, Identifier, or DisplayName) and refuses anything that isn't custom — the
// guardrail against deleting a commercial pack or the stock base integration.
func resolveCustomIntegration(ctx context.Context, c *soar.Client, key string) (soar.Integration, error) {
	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		return soar.Integration{}, err
	}
	var matches []soar.Integration
	for _, i := range ints {
		if i.Name == key || i.Identifier == key || i.DisplayName == key {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return soar.Integration{}, fmt.Errorf("no installed integration matches %q (try `soar integration list`)", key)
	case 1:
		if !soar.IsDeletableIntegration(matches[0]) {
			return soar.Integration{}, fmt.Errorf("integration %q is a stock base pack, not a custom pack or clone; only those are deletable", key)
		}
		return matches[0], nil
	default:
		return soar.Integration{}, fmt.Errorf("%q is ambiguous (%d matches); address the clone by its unique Name", key, len(matches))
	}
}

func newSOARIntegrationCreateCmd() *cobra.Command {
	var (
		integration string
		env         string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "create --integration <id> --environment <env>",
		Short: "Create a new, unconfigured (inert) integration instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("integration create", dryRun, yes)
			return mirror.PushSOARIntegrationCreate(baseContext(), lc, integration, env, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&env, "environment", "", "environment to scope the instance to (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("environment")
	return cmd
}

func newSOARIntegrationDeleteCmd() *cobra.Command {
	var (
		integration string
		env         string
		id          string
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration <id> --environment <env> --id <instance-id>",
		Short: "Delete an integration instance (warns if playbooks use it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard("integration delete", dryRun, yes)
			return mirror.PushSOARIntegrationDelete(baseContext(), lc, integration, env, id, dr, ay, os.Stdout)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "installed integration identifier (required)")
	f.StringVar(&env, "environment", "", "environment the instance is scoped to (required)")
	f.StringVar(&id, "id", "", "instance identifier to delete (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

// newSOARSettingsCmd groups the singleton case-routing policy get/set verbs.
// These are one-record settings (no list/id/delete), so they are imperative rather
// than reconcile surfaces.
func newSOARSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Read/set singleton SOAR case-routing policies",
	}
	cmd.AddCommand(
		newSOARPolicyCmd("case-assignment", "case auto-assignment policy", "assignmentPolicy",
			func(lc *legacy.Client) func(context.Context) (legacy.RawJSON, error) {
				return lc.GetCaseAssignmentPolicySettings
			},
			func(lc *legacy.Client) func(context.Context, any) (legacy.RawJSON, error) {
				return lc.AddOrUpdateCaseAssignmentPolicySettings
			}),
		newSOARPolicyCmd("move-case-policy", "cross-environment case-move policy", "moveCaseBetweenEnvironmentsPolicy",
			func(lc *legacy.Client) func(context.Context) (legacy.RawJSON, error) {
				return lc.GetMoveCaseBetweenEnvironmentsPolicySettings
			},
			func(lc *legacy.Client) func(context.Context, any) (legacy.RawJSON, error) {
				return lc.AddOrUpdateMoveCaseBetweenEnvironmentsPolicySettings
			}),
	)
	return cmd
}

// newSOARPolicyCmd builds a `get`/`set <value>` command pair for one singleton
// policy. value is the integer enum the policy accepts; a set is guarded.
func newSOARPolicyCmd(use, desc, field string,
	get func(*legacy.Client) func(context.Context) (legacy.RawJSON, error),
	set func(*legacy.Client) func(context.Context, any) (legacy.RawJSON, error)) *cobra.Command {
	parent := &cobra.Command{Use: use, Short: desc}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Print the current " + desc,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			return mirror.PrintSOARSettingSingleton(baseContext(), desc, get(lc), os.Stdout)
		},
	}

	var (
		dryRun bool
		yes    bool
	)
	setCmd := &cobra.Command{
		Use:   "set <value>",
		Short: "Set the " + desc + " (integer enum; guarded)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("value must be an integer enum: %w", err)
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			dr, ay := soarGuard(use+" set", dryRun, yes)
			return mirror.PushSOARSettingPolicy(baseContext(), desc, field, v, set(lc), dr, ay, os.Stdout)
		},
	}
	sf := setCmd.Flags()
	sf.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	sf.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	setCmd.MarkFlagsMutuallyExclusive("dry-run", "yes")

	parent.AddCommand(getCmd, setCmd)
	return parent
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

// parseCloseReason maps a reason name (or a raw int) to the typed CloseReason. Names
// are used over magic ints because the integer coding is the server's and is NOT
// alphabetical (Malicious=0) — a bare number is easy to get backwards.
func parseCloseReason(s string) (legacy.CloseReason, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "malicious":
		return legacy.CloseMalicious, nil
	case "not-malicious", "notmalicious", "not_malicious":
		return legacy.CloseNotMalicious, nil
	case "maintenance":
		return legacy.CloseMaintenance, nil
	case "inconclusive":
		return legacy.CloseInconclusive, nil
	case "unknown":
		return legacy.CloseUnknown, nil
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return legacy.CloseReason(n), nil
	}
	return 0, fmt.Errorf("invalid close reason %q (use malicious|not-malicious|maintenance|inconclusive|unknown)", s)
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
