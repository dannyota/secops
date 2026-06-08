package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
)

func init() {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Live smoke test: check config, auth, and SIEM/SOAR reachability (read-only)",
		Long: "doctor runs a quick end-to-end sanity check against the configured live\n" +
			"instance: it loads config, acquires a token, and makes one read-only call to\n" +
			"the SIEM (list rules) and, if soar_url is set, to SOAR (list integrations).\n" +
			"It never mutates anything. --json emits {ok, version, checks[]}.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runDoctor,
	}
	rootCmd.AddCommand(doctorCmd)
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
	var checks []doctorCheck

	inst, cfgErr := loadInstance()
	if cfgErr != nil {
		checks = append(checks, doctorCheck{Name: "config", label: "config", Error: cfgErr.Error()})
	} else {
		checks = append(checks, doctorCheck{Name: "config", label: "config", OK: true, Detail: inst.Region + " / " + inst.ProjectID})

		creds := auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4))
		var client *chronicle.Client

		auc := doctorCheck{Name: "auth", label: "auth (OAuth)", Hint: "run `gcloud auth application-default login` (or export SECOPS_ACCESS_TOKEN), then retry"}
		// A throwaway request object only to mint+attach the token (never sent);
		// host is derived from the configured region, not hard-coded.
		probe, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s-chronicle.googleapis.com/", inst.Region), nil)
		if err := creds.Apply(probe); err != nil {
			auc.Error = err.Error()
		} else if c, cerr := chronicle.NewClient(inst.Settings(), creds); cerr != nil {
			auc.Error = cerr.Error()
		} else {
			client, auc.OK, auc.Detail = c, true, "token acquired"
		}
		checks = append(checks, auc)

		if client != nil {
			sc := doctorCheck{Name: "siem", label: "SIEM reach", Hint: "check the region/project_id in your config and that the Chronicle API is enabled for the project"}
			if rules, err := client.ListRules(ctx); err != nil {
				sc.Error = err.Error()
			} else {
				sc.OK, sc.Detail = true, fmt.Sprintf("list rules OK (%d rule(s))", len(rules))
			}
			checks = append(checks, sc)
		} else {
			checks = append(checks, doctorCheck{Name: "siem", label: "SIEM reach", Skipped: true, Detail: "auth failed; skipped"})
		}

		if inst.SOARURL == "" {
			checks = append(checks, doctorCheck{Name: "soar", label: "SOAR reach", Skipped: true, Detail: "soar_url not set; skipped"})
		} else {
			soc := doctorCheck{Name: "soar", label: "SOAR reach", Hint: "check soar_url and the SOAR AppKey (soar_app_key in config or $SECOPS_SOAR_APP_KEY)"}
			if scl, err := newSOARClient(); err != nil {
				soc.Error = err.Error()
			} else if integs, err := scl.ListIntegrations(ctx); err != nil {
				soc.Error = err.Error()
			} else {
				soc.OK, soc.Detail = true, fmt.Sprintf("list integrations OK (%d)", len(integs))
			}
			checks = append(checks, soc)
		}
	}

	allOK := true
	for _, c := range checks {
		if !c.Skipped && !c.OK {
			allOK = false
		}
	}

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
		fmt.Printf("  %-13s    %s\n", "version", versionLine())
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
