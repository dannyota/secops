package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newInfoSOARSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soar-system",
		Short: "SOAR platform version, license, and data retention",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return preferModern("info soar-system",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					ctx := baseContext()
					ver, verr := mc.SystemGetVersion(ctx)
					lic, lerr := mc.SystemGetLicenseStatus(ctx)
					ret, rerr := mc.SystemGetMaxDataRetention(ctx)
					return renderSystemInfo(ver, verr, lic, lerr, ret, rerr)
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					ctx := baseContext()
					ver, verr := lc.SystemGetVersion(ctx)
					lic, lerr := lc.SystemGetLicenseStatus(ctx)
					ret, rerr := lc.SystemGetMaxDataRetention(ctx)
					return renderSystemInfo(ver, verr, lic, lerr, ret, rerr)
				},
			)
		},
	}
	return markJSON(cmd)
}

func renderSystemInfo(ver json.RawMessage, verr error, lic json.RawMessage, lerr error, ret json.RawMessage, rerr error) error {
	if jsonOut {
		out := map[string]any{}
		if verr == nil {
			out["version"] = ver
		}
		if lerr == nil {
			out["license"] = lic
		}
		if rerr == nil {
			out["maxDataRetention"] = ret
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
		return nil
	}
	fmt.Fprintln(os.Stdout, "SOAR system:")
	// Version and retention are wrapped in {"payload": ...}.
	printPayloadField("  version", ver, verr)
	printPayloadField("  max_data_retention_months", ret, rerr)
	// License has useful top-level scalars.
	if lerr != nil {
		fmt.Fprintf(os.Stdout, "  license: (error: %v)\n", lerr)
	} else {
		var l struct {
			ServerVersion string `json:"serverVersion"`
			LicenseType   string `json:"licenseType"`
			IsEvaluation  bool   `json:"isEvaluation"`
			RemainingDays int    `json:"licenceRemainingDays"`
			Users         int    `json:"usersInUsage"`
			Playbooks     int    `json:"playbooksInUsage"`
			Connectors    int    `json:"connectorsInUsage"`
			Environments  int    `json:"environmentsInUsage"`
			AlertsPerDay  int    `json:"alertsPerDayInUsage"`
			AlertsLimit   int    `json:"alertsPerDayLimit"`
		}
		if json.Unmarshal(lic, &l) == nil {
			fmt.Fprintf(os.Stdout, "  license_type: %s (eval=%v, %d days remaining)\n", l.LicenseType, l.IsEvaluation, l.RemainingDays)
			fmt.Fprintf(os.Stdout, "  usage: %d users, %d playbooks, %d connectors, %d envs, %d/%d alerts/day\n",
				l.Users, l.Playbooks, l.Connectors, l.Environments, l.AlertsPerDay, l.AlertsLimit)
		} else {
			printField("  license", lic, nil)
		}
	}
	return nil
}

func printPayloadField(label string, raw json.RawMessage, err error) {
	if err != nil {
		fmt.Fprintf(os.Stdout, "%s: (error: %v)\n", label, err)
		return
	}
	var wrapped struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Payload) > 0 {
		raw = wrapped.Payload
	}
	printField(label, raw, nil)
}

func printField(label string, raw json.RawMessage, err error) {
	if err != nil {
		fmt.Fprintf(os.Stdout, "%s: (error: %v)\n", label, err)
		return
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		fmt.Fprintf(os.Stdout, "%s: -\n", label)
		return
	}
	// Unquote a simple string.
	var str string
	if json.Unmarshal(raw, &str) == nil {
		fmt.Fprintf(os.Stdout, "%s: %s\n", label, str)
		return
	}
	fmt.Fprintf(os.Stdout, "%s: %s\n", label, s)
}
