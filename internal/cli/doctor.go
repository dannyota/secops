package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
)

func init() {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, auth, and API connectivity (read-only)",
		Long: "doctor checks that secopsctl is correctly configured and can reach both\n" +
			"APIs. It validates the config file (existence, permissions, required fields),\n" +
			"acquires auth credentials, and makes one lightweight read-only call to the\n" +
			"SIEM and SOAR planes. It never mutates anything. --json emits {ok, version, checks[]}.",
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
	ctx := baseContext()
	checks, allOK, cfgErr := healthChecks(ctx)

	if jsonOut {
		if err := emitJSON(struct {
			OK      bool          `json:"ok"`
			Version string        `json:"version"`
			Checks  []doctorCheck `json:"checks"`
		}{OK: allOK, Version: versionLine(), Checks: checks}); err != nil {
			return err
		}
	} else {
		fmt.Println("secopsctl doctor")
		fmt.Printf("  %-16s %s\n", "version", versionLine())
		for _, c := range checks {
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

// healthChecks runs the config/auth/SIEM/SOAR probes and returns the per-check
// outcomes, an all-passed flag, and the config error (if config itself failed).
// Shared by `doctor` and the `capabilities` session-bootstrap probe.
func healthChecks(ctx context.Context) (checks []doctorCheck, allOK bool, cfgErr error) {
	var inst *config.Instance
	inst, cfgErr = loadInstance()
	if cfgErr != nil {
		checks = append(checks, doctorCheck{
			Name:  "config",
			label: "config",
			Error: cfgErr.Error(),
			Hint:  "run `secopsctl config` to create or fix the config file",
		})
		return finalize(checks)
	}

	checks = append(checks, doctorCheck{
		Name:   "config",
		label:  "config",
		OK:     true,
		Detail: inst.Region + " / " + inst.ProjectID,
	})

	checks = append(checks, checkConfigFields(inst))

	// Run the SIEM pipeline (auth → siem) and SOAR probe in parallel —
	// the two planes use independent hosts and credentials.
	var (
		wg    sync.WaitGroup
		authC doctorCheck
		siemC doctorCheck
		soarC doctorCheck
	)

	wg.Add(2)

	// SIEM pipeline: auth then siem (sequential within this goroutine).
	go func() {
		defer wg.Done()
		authC = checkAuth(ctx, inst)
		var client *chronicle.Client
		if authC.OK {
			// Auth passed — build client for the SIEM reach check.
			creds := auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4))
			if c, err := chronicle.NewClient(inst.Settings(), creds); err == nil {
				client = c
			}
		}
		siemC = checkSIEM(ctx, client, inst)
	}()

	// SOAR probe: fully independent (AppKey auth, different host).
	go func() {
		defer wg.Done()
		soarC = checkSOAR(ctx, inst)
	}()

	wg.Wait()

	checks = append(checks, authC, siemC, soarC)
	return finalize(checks)
}

func checkAuth(ctx context.Context, inst *config.Instance) doctorCheck {
	c := doctorCheck{Name: "auth", label: "auth (OAuth)"}
	creds := auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4))
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
	c.OK, c.Detail = true, "all fields set"
	return c
}

func checkSIEM(ctx context.Context, client *chronicle.Client, inst *config.Instance) doctorCheck {
	c := doctorCheck{Name: "siem", label: "SIEM reach"}
	if client == nil {
		c.Skipped, c.Detail = true, "auth failed; skipped"
		return c
	}
	if _, err := client.ListRulesBasic(ctx); err != nil {
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

func checkSOAR(ctx context.Context, inst *config.Instance) doctorCheck {
	c := doctorCheck{Name: "soar", label: "SOAR reach"}
	if inst.SOARURL == "" {
		c.Skipped, c.Detail = true, "soar_url not set; skipped"
		return c
	}
	scl, err := newSOARClient()
	if err != nil {
		c.Error = err.Error()
		c.Hint = "set soar_app_key in config (or $SECOPS_SOAR_APP_KEY)"
		return c
	}
	if _, err := scl.ListIntegrations(ctx); err != nil {
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
