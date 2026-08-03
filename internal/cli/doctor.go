package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
	"danny.vn/secops/soar"
)

const (
	// Doctor is an interactive health check, not a bulk operation. Keep its
	// default command-wide deadline short even though normal API requests default
	// to 60 seconds; an explicit --timeout overrides this (0 disables it).
	defaultDoctorTimeout = 10 * time.Second
	doctorSOARHedgeDelay = 500 * time.Millisecond
)

func init() {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, auth, and API connectivity (read-only)",
		Long: "doctor checks that secopsctl is correctly configured and can reach its\n" +
			"APIs. It validates the config file (existence, permissions, required fields),\n" +
			"acquires auth credentials, and makes one lightweight read-only call to the\n" +
			"SIEM plane and, when configured, the SOAR plane. It never mutates anything.\n" +
			"By default all health probes must finish within 10s; --timeout overrides that\n" +
			"doctor-wide deadline (0 disables it).\n" +
			"--json emits {ok, version, checks[]}.",
		Example: "  secopsctl doctor        # human-readable\n" +
			"  secopsctl doctor --json # machine-readable (CI / monitoring)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runDoctor,
	}
	rootCmd.AddCommand(markJSON(doctorCmd))
}

// doctorCheck is one health check's outcome. Name is the machine key (--json);
// label is the human heading for the text view; hint is shown on failure.
type doctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
	Hint    string `json:"hint,omitempty"`

	label string // text-view heading (not serialized)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	var report func(doctorCheck)
	if !jsonOut {
		fmt.Println("secopsctl doctor")
		fmt.Printf("  %-16s %s\n", "version", versionLine())
		report = printDoctorCheck
	}

	checks, allOK, cfgErr := healthChecks(baseContext(), effectiveDoctorTimeout(cmd), report)

	if jsonOut {
		if err := emitJSON(struct {
			OK      bool          `json:"ok"`
			Version string        `json:"version"`
			Checks  []doctorCheck `json:"checks"`
		}{OK: allOK, Version: versionLine(), Checks: checks}); err != nil {
			return err
		}
	} else {
		fmt.Println()
		if allOK {
			fmt.Println("  → all checks passed.")
		} else {
			fmt.Println("  → some checks failed.")
		}
	}

	if cfgErr != nil {
		return errors.New("doctor: config check failed")
	}
	if !allOK {
		return errors.New("doctor: one or more checks failed")
	}
	return nil
}

func printDoctorCheck(c doctorCheck) {
	switch {
	case c.Skipped:
		fmt.Printf("  %-13s -  %s\n", c.label, c.Detail)
	case c.OK:
		fmt.Printf("  %-13s ✓  %s\n", c.label, c.Detail)
	default:
		fmt.Printf("  %-13s ✗  %s\n", c.label, c.Error)
		if c.Hint != "" {
			fmt.Printf("  %-13s    ↳ %s\n", "", c.Hint)
		}
	}
}

func effectiveDoctorTimeout(cmd *cobra.Command) time.Duration {
	if flag := cmd.Flag("timeout"); flag != nil && flag.Changed {
		return requestTimeout
	}
	return defaultDoctorTimeout
}

// healthChecks runs the config/auth/SIEM/SOAR probes and returns the per-check
// outcomes, an all-passed flag, and the config error (if config itself failed).
// Shared by `doctor` and the `capabilities` session-bootstrap probe.
func healthChecks(
	ctx context.Context,
	timeout time.Duration,
	report func(doctorCheck),
) (checks []doctorCheck, allOK bool, cfgErr error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	emit := func(c doctorCheck) {
		checks = append(checks, c)
		if report != nil {
			report(c)
		}
	}

	var inst *config.Instance
	inst, cfgErr = loadInstance()
	if cfgErr != nil {
		emit(doctorCheck{
			Name:  "config",
			label: "config",
			Error: cfgErr.Error(),
			Hint:  "run `secopsctl config` to create or fix the config file",
		})
		return finalize(checks)
	}

	emit(doctorCheck{
		Name:   "config",
		label:  "config",
		OK:     true,
		Detail: inst.Region + " / " + inst.ProjectID,
	})

	emit(checkConfigFields(inst))

	// Run the SIEM pipeline (auth → siem) and SOAR probe in parallel —
	// the two planes use independent hosts and credentials. Results come back on
	// one channel so progress rendering is serialized while final JSON ordering
	// remains stable.
	type indexedCheck struct {
		index int
		check doctorCheck
	}
	results := make(chan indexedCheck, 3)
	const (
		authIndex = iota
		siemIndex
		soarIndex
	)

	// SIEM pipeline: auth then siem (sequential within this goroutine).
	go func() {
		creds := auth.OAuth(
			auth.WithForceIPv4(inst.ForceIPv4),
			auth.WithTokenContext(ctx),
		)
		authC := checkAuth(ctx, inst, creds)
		results <- indexedCheck{index: authIndex, check: authC}

		var client *chronicle.Client
		if authC.OK {
			// Reuse the resolved OAuth credentials and apply the same short bound to
			// the API exchange; the command-wide context also caps SDK retries.
			if c, err := chronicle.NewClient(inst.Settings(), creds,
				chronicle.WithHTTPClient(timeoutHTTPClient(creds, inst.ForceIPv4, timeout))); err == nil {
				client = c
			}
		}
		results <- indexedCheck{index: siemIndex, check: checkSIEM(ctx, client, inst)}
	}()

	// SOAR probe: fully independent (AppKey auth, different host).
	go func() {
		results <- indexedCheck{index: soarIndex, check: checkSOAR(ctx, inst, timeout)}
	}()

	ordered := make([]doctorCheck, 3)
	for range ordered {
		result := <-results
		ordered[result.index] = result.check
		if report != nil {
			report(result.check)
		}
	}
	checks = append(checks, ordered...)
	return finalize(checks)
}

func checkAuth(ctx context.Context, inst *config.Instance, creds auth.Credentials) doctorCheck {
	c := doctorCheck{Name: "auth", label: "auth (OAuth)"}
	if err := ctx.Err(); err != nil {
		c.Error = err.Error()
		c.Hint = "raise --timeout if credential discovery needs longer"
		return c
	}
	probe, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://%s-chronicle.googleapis.com/", inst.Region), nil)
	if err := creds.Apply(probe); err != nil {
		c.Error = err.Error()
		c.Hint = "run `gcloud auth application-default login`, then retry"
		return c
	}
	c.OK, c.Detail = true, "token acquired"
	return c
}

func finalize(checks []doctorCheck) ([]doctorCheck, bool, error) {
	allOK := true
	var cfgErr error
	for _, c := range checks {
		if !c.Skipped && !c.OK {
			allOK = false
			if c.Name == "config" {
				cfgErr = errors.New(c.Error)
			}
		}
	}
	return checks, allOK, cfgErr
}

func checkConfigFields(inst *config.Instance) doctorCheck {
	c := doctorCheck{Name: "config_fields", label: "config fields"}
	if inst.SOARURL == "" && inst.SOARAppKey == "" {
		c.Skipped, c.Detail = true, "SOAR not configured; SIEM-only"
		return c
	}
	var missing []string
	if inst.SOARURL == "" {
		missing = append(missing, "soar_url")
	}
	if inst.SOARAppKey == "" {
		missing = append(missing, "soar_app_key")
	}
	if len(missing) > 0 {
		c.Error = "missing: " + strings.Join(missing, ", ")
		c.Hint = "run `secopsctl config` to set them; SOAR commands need both"
		return c
	}
	c.OK, c.Detail = true, "SIEM and SOAR fields set"
	return c
}

func checkSIEM(ctx context.Context, client *chronicle.Client, inst *config.Instance) doctorCheck {
	c := doctorCheck{Name: "siem", label: "SIEM reach"}
	if client == nil {
		c.Skipped, c.Detail = true, "auth failed; skipped"
		return c
	}
	if _, err := client.GetInstance(ctx); err != nil {
		c.Error = err.Error()
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			c.Hint = "ADC identity lacks SecOps SIEM permission — check IAM roles (Chronicle API Viewer or higher)"
		} else {
			c.Hint = "check region/project_id in config and that the Chronicle API is enabled"
		}
		return c
	}
	c.OK, c.Detail = true, inst.Region+"-chronicle.googleapis.com"
	return c
}

func checkSOAR(ctx context.Context, inst *config.Instance, timeout time.Duration) doctorCheck {
	c := doctorCheck{Name: "soar", label: "SOAR reach"}
	if inst.SOARURL == "" {
		c.Skipped, c.Detail = true, "soar_url not set; skipped"
		return c
	}
	if err := probeSOARProtocols(ctx, inst, timeout, doctorSOARHedgeDelay, auth.HTTPTransport); err != nil {
		c.Error = err.Error()
		c.Hint = "check soar_url and soar_app_key in config (or $SECOPS_SOAR_APP_KEY)"
		return c
	}
	host := inst.SOARURL
	if u, perr := url.Parse(host); perr == nil && u.Host != "" {
		host = u.Host
	}
	c.OK, c.Detail = true, host
	return c
}

type doctorTransportFactory func(forceIPv4 bool) *http.Transport

// probeSOARProtocols checks one tiny authenticated endpoint. The normal
// HTTP/2-preferred lane starts first; if it has not completed promptly, an
// independent forced-HTTP/1.1 lane starts. First success cancels its duplicate.
func probeSOARProtocols(
	ctx context.Context,
	inst *config.Instance,
	timeout time.Duration,
	hedgeDelay time.Duration,
	newTransport doctorTransportFactory,
) error {
	settings := soarSettings(inst)
	key, err := soarAppKey(inst)
	if err != nil {
		return err
	}
	creds := auth.SOARAppKey(key)

	primaryTransport := newTransport(settings.ForceIPv4)
	fallbackTransport := newTransport(settings.ForceIPv4)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	fallbackTransport.Protocols = protocols
	fallbackTransport.ForceAttemptHTTP2 = false
	if fallbackTransport.TLSClientConfig != nil {
		// Protocols controls net/http's selection, while NextProtos controls TLS
		// ALPN. Keep them aligned even when a custom transport factory supplied an
		// h2-preferring TLS config (tests and embedders commonly do).
		tlsConfig := fallbackTransport.TLSClientConfig.Clone()
		tlsConfig.NextProtos = []string{"http/1.1"}
		fallbackTransport.TLSClientConfig = tlsConfig
	}
	defer primaryTransport.CloseIdleConnections()
	defer fallbackTransport.CloseIdleConnections()

	newProbeClient := func(transport *http.Transport) (*soar.Client, error) {
		return soar.NewClient(settings, creds,
			soar.WithHTTPClient(&http.Client{
				Timeout:   timeout,
				Transport: auth.RoundTripper(creds, transport),
			}),
			soar.WithoutRetries(),
		)
	}
	primary, err := newProbeClient(primaryTransport)
	if err != nil {
		return err
	}
	fallback, err := newProbeClient(fallbackTransport)
	if err != nil {
		return err
	}

	return runHedgedDoctorProbe(ctx, boundedDoctorHedgeDelay(ctx, hedgeDelay),
		func(ctx context.Context) error {
			_, err := primary.SystemGetVersion(ctx)
			if err != nil {
				return fmt.Errorf("HTTP/2-preferred: %w", err)
			}
			return nil
		},
		func(ctx context.Context) error {
			_, err := fallback.SystemGetVersion(ctx)
			if err != nil {
				return fmt.Errorf("HTTP/1.1: %w", err)
			}
			return nil
		},
	)
}

// boundedDoctorHedgeDelay guarantees the fallback has time to run even when an
// operator explicitly chooses a deadline shorter than the normal hedge delay.
// Half the remaining budget goes to the preferred lane, half to the fallback.
func boundedDoctorHedgeDelay(ctx context.Context, delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return delay
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	return min(delay, remaining/2)
}
