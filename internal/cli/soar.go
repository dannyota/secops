package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
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

// resolveSOARToken returns the SOAR-host bearer token, or "" when none is set. A
// token lets a SOAR call authenticate as the web console does (Authorization:
// Bearer) instead of with the AppKey header — needed for surfaces the AppKey
// identity can't run. The --soar-token flag wins over $SECOPS_SOAR_TOKEN, and the
// value may be a literal, an "env:VAR" indirection, or "@/path/to/file" — the
// indirections keep a sensitive, short-lived session token out of the shell history
// and the process argument list.
func resolveSOARToken() (string, error) {
	raw := strings.TrimSpace(soarToken)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("SECOPS_SOAR_TOKEN"))
	}
	if raw == "" {
		return "", nil
	}
	switch {
	case strings.HasPrefix(raw, "env:"):
		name := strings.TrimSpace(strings.TrimPrefix(raw, "env:"))
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			return "", fmt.Errorf("--soar-token references env var %q which is empty or unset", name)
		}
		return v, nil
	case strings.HasPrefix(raw, "@"):
		path := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read --soar-token file %q: %w", path, err)
		}
		tok := strings.TrimSpace(string(b))
		if tok == "" {
			return "", fmt.Errorf("--soar-token file %q is empty", path)
		}
		return tok, nil
	default:
		return raw, nil
	}
}

// resolveSOARCreds picks the SOAR-host credential and a human label for it. A SOAR
// bearer token (resolveSOARToken) takes precedence and is sent as Authorization:
// Bearer — exactly how the web console authenticates — otherwise the long-lived
// AppKey is used. When a token is supplied the AppKey is not required, so a
// token-only session works without one configured.
func resolveSOARCreds(inst *config.Instance) (auth.Credentials, string, error) {
	tok, err := resolveSOARToken()
	if err != nil {
		return nil, "", err
	}
	if tok != "" {
		return auth.BearerToken(tok), "bearer token (JWT)", nil
	}
	key, err := soarAppKey(inst)
	if err != nil {
		return nil, "", err
	}
	return auth.SOARAppKey(key), "AppKey", nil
}

// newSOARSettings loads the instance, derives SOAR settings, and resolves the
// active SOAR credential (bearer token when set, else AppKey).
func newSOARSettings() (soar.Settings, auth.Credentials, error) {
	inst, err := loadInstance()
	if err != nil {
		return soar.Settings{}, nil, err
	}
	s := soarSettings(inst)
	if s.BaseURL == "" {
		return soar.Settings{}, nil, fmt.Errorf("soar_url is not set in the instance config (the tenant SOAR host)")
	}
	creds, _, err := resolveSOARCreds(inst)
	if err != nil {
		return soar.Settings{}, nil, err
	}
	return s, creds, nil
}

func newSOARClient() (*soar.Client, error) {
	s, creds, err := newSOARSettings()
	if err != nil {
		return nil, err
	}
	return soar.NewClient(s, creds, soar.WithHTTPClient(timedHTTPClient(creds, s.ForceIPv4)))
}

func newSOARLegacyClient() (*legacy.Client, error) {
	s, creds, err := newSOARSettings()
	if err != nil {
		return nil, err
	}
	return legacy.NewClient(s, creds, timedHTTPClient(creds, s.ForceIPv4)), nil
}

func init() {
	soarCmd := &cobra.Command{
		Use:   "soar",
		Short: "Operate Google SecOps SOAR (Siemplify) as code (AppKey auth, no ADC)",
		Long: "Operate the SOAR surface: read-only `pull` of a wide set of config\n" +
			"surfaces (connectors, jobs, grouping rules, cases, playbooks, webhooks,\n" +
			"environments, networks, SLA, and more — run `soar pull --help` for the\n" +
			"full list) and guarded mutating `push`. SOAR uses a long-lived AppKey\n" +
			"($SECOPS_SOAR_APP_KEY) and the soar_url config host (no ADC).\n\n" +
			"Auth override: pass --soar-token (or $SECOPS_SOAR_TOKEN) a SOAR-host bearer\n" +
			"token — e.g. a session JWT copied from the web console — to authenticate SOAR\n" +
			"calls as the console does (Authorization: Bearer) instead of with the AppKey.\n" +
			"Use it for surfaces the AppKey identity can't run (see `soar case run-action`).",
	}
	soarCmd.AddCommand(newSOARPullCmd(), newSOARPushCmd(), newSOARCaseCmd(), newSOARLegacyCmd(),
		newSOARIntegrationCmd(), newSOARSettingsCmd(), newSOARMarketplaceCmd(), newSOARUsersCmd(),
		newSOARPlaybookCmd(), newSOARJobCmd(), newSOARPackageIntegrationCmd(), newSOARBuildPlaybookCmd(),
		newSOARAuditCmd(), newSOARConnectorCmd())
	rootCmd.AddCommand(soarCmd)
}

// soarGuard derives the dry-run / confirmation state from the standard flags and
// the interactive prompt, mirroring `push`. In read-only mode every mutation
// degrades to a dry run (--yes or not); a confirmed mutation is recorded in the
// local audit log. (The decision core is the shared deriveGuard.)
func soarGuard(target string, dryRunFlag, yesFlag bool) (dryRun, assumeYes bool) {
	return deriveGuard(target, dryRunFlag, yesFlag)
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
