package cli

import (
	"fmt"
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
	creds := auth.SOARAppKey(key)
	return soar.NewClient(s, creds, soar.WithHTTPClient(timedHTTPClient(creds, s.ForceIPv4)))
}

func newSOARLegacyClient() (*legacy.Client, error) {
	s, key, err := newSOARSettings()
	if err != nil {
		return nil, err
	}
	creds := auth.SOARAppKey(key)
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
			"($SECOPS_SOAR_APP_KEY) and the soar_url config host (no ADC).",
	}
	soarCmd.AddCommand(newSOARPullCmd(), newSOARPushCmd(), newSOARLegacyCmd(),
		newSOARIntegrationCmd(), newSOARSettingsCmd(), newSOARUsersCmd(),
		newSOARPlaybookCmd(), newSOARJobCmd(), newSOARIDECmd(),
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
