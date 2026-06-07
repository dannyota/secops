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
			"It never mutates anything.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runDoctor,
	}
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := baseContext()
	allOK := true

	step := func(name string, fn func() (string, error)) {
		detail, err := fn()
		if err != nil {
			allOK = false
			fmt.Printf("  %-13s ✗  %v\n", name, err)
			return
		}
		fmt.Printf("  %-13s ✓  %s\n", name, detail)
	}
	skip := func(name, why string) {
		fmt.Printf("  %-13s -  %s\n", name, why)
	}

	fmt.Println("secopsctl doctor")
	fmt.Printf("  %-13s    %s\n", "version", versionLine())

	inst, err := loadInstance()
	if err != nil {
		fmt.Printf("  %-13s ✗  %v\n", "config", err)
		return errors.New("doctor: config check failed")
	}
	fmt.Printf("  %-13s ✓  %s / %s\n", "config", inst.Region, inst.ProjectID)

	// Auth: acquire a token without sending any request (Apply mints it via the
	// Google auth library — no gcloud shell-out, no token persisted).
	creds := auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4))
	var client *chronicle.Client
	step("auth (OAuth)", func() (string, error) {
		// A throwaway request object only to mint+attach the token (never sent);
		// host is derived from the configured region, not hard-coded.
		probeURL := fmt.Sprintf("https://%s-chronicle.googleapis.com/", inst.Region)
		probe, _ := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
		if err := creds.Apply(probe); err != nil {
			return "", err
		}
		c, cerr := chronicle.NewClient(inst.Settings(), creds)
		if cerr != nil {
			return "", cerr
		}
		client = c
		return "token acquired", nil
	})

	if client != nil {
		step("SIEM reach", func() (string, error) {
			rules, err := client.ListRules(ctx)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("list rules OK (%d rule(s))", len(rules)), nil
		})
	} else {
		skip("SIEM reach", "auth failed; skipped")
	}

	if inst.SOARURL == "" {
		skip("SOAR reach", "soar_url not set; skipped")
	} else {
		step("SOAR reach", func() (string, error) {
			sc, err := newSOARClient()
			if err != nil {
				return "", err
			}
			integs, err := sc.ListIntegrations(ctx)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("list integrations OK (%d)", len(integs)), nil
		})
	}

	fmt.Println()
	if !allOK {
		fmt.Println("  → some checks failed.")
		return errors.New("doctor: one or more checks failed")
	}
	fmt.Println("  → all checks passed.")
	return nil
}
