package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"danny.vn/secops/config"
)

func init() {
	var (
		fProjectID     string
		fProjectNumber string
		fRegion        string
		fCustomerID    string
		fSOARURL       string
		fSOARAppKey    string
		fForceIPv4     bool
		fShowPath      bool
	)

	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"init"},
		Short:   "Set up the secopsctl config (~/.secopsctl/instance.yaml)",
		Long: "Create or edit the instance config in a single-screen form: all fields on\n" +
			"one screen, ↑/↓ or Tab to move, edit in place, then Save (or Cancel). The\n" +
			"SOAR AppKey field is hidden. Flags set values directly; --non-interactive\n" +
			"(or non-terminal stdin) skips the form and writes flags + current values.\n\n" +
			"Writes ~/.secopsctl/instance.yaml (0600), or the --config path if given.\n" +
			"The file may hold the SOAR AppKey in plaintext (v1); it is git-ignored and\n" +
			"never committed. At run time, real SECOPS_* env vars override the file. The\n" +
			"mintable OAuth/ADC SIEM token is never stored — `gcloud auth\n" +
			"application-default login` handles SIEM auth.",
		Example: "  # open the interactive setup form\n" +
			"  secopsctl config\n\n" +
			"  # set values non-interactively (e.g. in a script)\n" +
			"  secopsctl config --non-interactive \\\n" +
			"      --project-id your-project-id --region us --customer-id 00000000-0000-0000-0000-000000000000\n\n" +
			"  # print the resolved config file path\n" +
			"  secopsctl config --show-path",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fShowPath {
				p, err := config.ResolvedSource(cfgFile)
				if err != nil {
					return err
				}
				if p == "" {
					fmt.Println("(no config file — using environment / defaults)")
				} else {
					fmt.Println(p)
				}
				return nil
			}
			target := cfgFile
			if target == "" {
				target = config.DefaultPath()
			}
			cur := config.ReadForEdit(target)

			// Flags override the current value when explicitly set.
			f := cmd.Flags()
			applyStringFlag(f, "project-id", fProjectID, &cur.ProjectID)
			if f.Changed("project-number") {
				cur.SetProjectNumber(fProjectNumber)
			}
			applyStringFlag(f, "region", fRegion, &cur.Region)
			applyStringFlag(f, "customer-id", fCustomerID, &cur.CustomerID)
			applyStringFlag(f, "soar-url", fSOARURL, &cur.SOARURL)
			applyStringFlag(f, "soar-app-key", fSOARAppKey, &cur.SOARAppKey)
			if f.Changed("force-ipv4") {
				cur.ForceIPv4 = fForceIPv4
			}

			if !nonInteractive && term.IsTerminal(int(os.Stdin.Fd())) {
				saved, err := runConfigForm(cur)
				if err != nil {
					return err
				}
				if !saved {
					fmt.Println("Cancelled; no changes written.")
					return nil
				}
			} else if cur.Region != "" && !config.IsKnownRegion(cur.Region) {
				// Non-interactive: warn but don't block (allows a region newer than
				// our list); the form path enforces membership.
				fmt.Fprintf(os.Stderr, "  (warn) region %q is not in the known list\n", cur.Region)
			}

			// Store the SOAR URL canonically (add https:// if the user typed a
			// bare host, trim a trailing slash).
			cur.SOARURL = normalizeSOARURL(cur.SOARURL)

			if missing := requiredMissing(cur); len(missing) > 0 {
				return fmt.Errorf("missing required value(s): %s", strings.Join(missing, ", "))
			}

			path, err := config.Save(cur, target)
			if err != nil {
				return err
			}
			fmt.Printf("Wrote config to %s\n", path)
			if cur.SOARAppKey != "" {
				fmt.Println("(SOAR AppKey stored in plaintext; the file is 0600 and git-ignored.)")
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&fProjectID, "project-id", "", "GCP project ID")
	f.StringVar(&fProjectNumber, "project-number", "", "GCP project number")
	f.StringVar(&fRegion, "region", "", "SecOps region (e.g. us, asia-southeast1)")
	f.StringVar(&fCustomerID, "customer-id", "", "SecOps customer ID (GUID)")
	f.StringVar(&fSOARURL, "soar-url", "", "SOAR host URL (optional)")
	f.StringVar(&fSOARAppKey, "soar-app-key", "", "SOAR AppKey (optional; avoid on shared shells — prefer the prompt)")
	f.BoolVar(&fForceIPv4, "force-ipv4", false, "pin the network dialer to IPv4 (corporate-VPN / broken-IPv6 fix)")
	f.BoolVar(&fShowPath, "show-path", false, "print the resolved config file path and exit (no changes)")

	rootCmd.AddCommand(cmd)
}

// runConfigForm shows the single-screen huh form pre-filled from cur, mutating
// cur in place. Returns whether the user chose Save.
func runConfigForm(cur *config.Instance) (bool, error) {
	projNum := cur.ProjectNumberString()
	save := true

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("secopsctl config").
				Description("Edit fields, then Save. Writes ~/.secopsctl/instance.yaml (0600)."),
			huh.NewInput().Title("Project ID").Value(&cur.ProjectID).
				Validate(requiredField("project ID")),
			huh.NewInput().Title("Project number").Value(&projNum).
				Validate(requiredField("project number")),
			huh.NewInput().Title("Region").Description("e.g. us, europe, asia-southeast1").
				Value(&cur.Region).Validate(validRegion),
			huh.NewInput().Title("Customer ID").Description("Chronicle instance GUID").
				Value(&cur.CustomerID).Validate(requiredField("customer ID")),
			huh.NewInput().Title("SOAR URL").Description("optional; for `soar` commands").
				Value(&cur.SOARURL),
			huh.NewInput().Title("SOAR AppKey").Description("optional; hidden").
				EchoMode(huh.EchoModePassword).Value(&cur.SOARAppKey),
			huh.NewConfirm().Title("Force IPv4?").
				Description("pin the dialer to IPv4 (corporate-VPN / broken-IPv6 fix)").
				Value(&cur.ForceIPv4),
			huh.NewConfirm().Title("Save this config?").
				Affirmative("Save").Negative("Cancel").Value(&save),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}

	cur.ProjectID = strings.TrimSpace(cur.ProjectID)
	cur.SetProjectNumber(strings.TrimSpace(projNum))
	cur.Region = strings.TrimSpace(cur.Region)
	cur.CustomerID = strings.TrimSpace(cur.CustomerID)
	cur.SOARURL = strings.TrimSpace(cur.SOARURL)
	cur.SOARAppKey = strings.TrimSpace(cur.SOARAppKey)
	return save, nil
}

// requiredField returns a huh validator that rejects an empty/blank value.
func requiredField(name string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

// validRegion rejects an empty or unrecognized region (see config.KnownRegions).
func validRegion(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("region is required")
	}
	if !config.IsKnownRegion(s) {
		return fmt.Errorf("unknown region %q (known: %s)", s, strings.Join(config.KnownRegions, ", "))
	}
	return nil
}

// requiredMissing lists the required identifiers still empty after flags/form.
func requiredMissing(i *config.Instance) []string {
	var missing []string
	if i.ProjectID == "" {
		missing = append(missing, "project_id")
	}
	if i.ProjectNumber == "" {
		missing = append(missing, "project_number")
	}
	if i.Region == "" {
		missing = append(missing, "region")
	}
	if i.CustomerID == "" {
		missing = append(missing, "customer_id")
	}
	return missing
}

// applyStringFlag copies the flag value into dst only when the flag was set.
func applyStringFlag(f *pflag.FlagSet, name, val string, dst *string) {
	if f.Changed(name) {
		*dst = val
	}
}
