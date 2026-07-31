package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
		Long: "Create or edit the instance config in an interactive form: click a field or\n" +
			"use ↑/↓ or Tab to move, edit in place, then click Save (or Cancel). The\n" +
			"mouse wheel moves through fields when the form is taller than the terminal.\n" +
			"The SOAR AppKey field is hidden. Flags set values directly;\n" +
			"--non-interactive (or non-terminal stdin) skips the form and writes flags +\n" +
			"current values.\n\n" +
			"Writes ~/.secopsctl/instance.yaml (0600), or the --config path if given.\n" +
			"The file may hold the SOAR AppKey in plaintext (v1); it is git-ignored and\n" +
			"never committed. At run time, real SECOPS_* env vars override the file. The\n" +
			"mintable OAuth/ADC SIEM token is never stored — `gcloud auth\n" +
			"application-default login` handles SIEM auth.",
		Example: "  # open the interactive setup form\n" +
			"  secopsctl config\n\n" +
			"  # set values non-interactively (e.g. in a script)\n" +
			"  secopsctl config --non-interactive \\\n" +
			"      --project-id your-project-id --project-number 000000000000 \\\n" +
			"      --region us --customer-id 00000000-0000-0000-0000-000000000000\n\n" +
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
			cur, err := config.ReadForEditStrict(target)
			if err != nil {
				return err
			}

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

			stdinTerminal := term.IsTerminal(int(os.Stdin.Fd()))
			stderrTerminal := term.IsTerminal(int(os.Stderr.Fd()))
			if !nonInteractive && stdinTerminal && !stderrTerminal {
				return fmt.Errorf("interactive config needs a terminal on stderr; remove the stderr redirect or use --non-interactive")
			}
			interactive := !nonInteractive && stdinTerminal && stderrTerminal
			if interactive {
				saved, err := runConfigForm(cur)
				if err != nil {
					return err
				}
				if !saved {
					fmt.Println("Cancelled; no changes written.")
					return nil
				}
			}

			normalizeConfigValues(cur)
			if !interactive && cur.Region != "" && !config.IsKnownRegion(cur.Region) {
				// Non-interactive: warn but don't block (allows a region newer than
				// our list); the form path enforces membership.
				fmt.Fprintf(os.Stderr, "  (warn) region %q is not in the known list\n", cur.Region)
			}

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
	f.StringVar(&fSOARURL, "soar-url", "", "SOAR host URL (e.g. https://<tenant>.siemplify-soar.com)")
	f.StringVar(&fSOARAppKey, "soar-app-key", "", "SOAR AppKey (avoid on shared shells — prefer the prompt)")
	f.BoolVar(&fForceIPv4, "force-ipv4", false, "pin the network dialer to IPv4 (corporate-VPN / broken-IPv6 fix)")
	f.BoolVar(&fShowPath, "show-path", false, "print the resolved config file path and exit (no changes)")

	rootCmd.AddCommand(cmd)
}

// runConfigForm shows the Huh form pre-filled from cur, mutating cur in place.
// Returns whether the user chose Save.
func runConfigForm(cur *config.Instance) (bool, error) {
	projNum := cur.ProjectNumberString()
	save := true
	mouseCancelled := false

	form, fields := newConfigForm(cur, &projNum, &save)
	mouse := &configMouseHandler{
		fields:         fields,
		save:           &save,
		mouseCancelled: &mouseCancelled,
	}
	form.WithOutput(os.Stderr)
	form.WithProgramOptions(
		tea.WithOutput(os.Stderr),
		tea.WithReportFocus(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithFilter(mouse.filter),
	)
	if err := form.Run(); err != nil {
		if mouseCancelled && errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}

	cur.SetProjectNumber(projNum)
	return save, nil
}

// newConfigForm builds the pointer-bound form separately from its runner so
// mouse behavior can be exercised with ordinary Bubble Tea messages in tests.
func newConfigForm(cur *config.Instance, projNum *string, save *bool) (*huh.Form, []huh.Field) {
	fields := []huh.Field{
		huh.NewInput().Key(configFieldProjectID).Title("Project ID").Value(&cur.ProjectID).
			Validate(requiredField("project ID")),
		huh.NewInput().Key(configFieldProjectNumber).Title("Project number").Value(projNum).
			Validate(requiredField("project number")),
		huh.NewInput().Key(configFieldRegion).Title("Region").
			Description("e.g. us, europe, asia-southeast1").
			Value(&cur.Region).Validate(validRegion),
		huh.NewInput().Key(configFieldCustomerID).Title("Customer ID").
			Description("Chronicle instance GUID").
			Value(&cur.CustomerID).Validate(requiredField("customer ID")),
		huh.NewInput().Key(configFieldSOARURL).Title("SOAR URL").
			Description("optional; needed for SOAR commands").
			Value(&cur.SOARURL),
		huh.NewInput().Key(configFieldSOARAppKey).Title("SOAR AppKey").
			Description("optional and hidden; needed for SOAR commands").
			EchoMode(huh.EchoModePassword).Value(&cur.SOARAppKey),
		huh.NewConfirm().Key(configFieldForceIPv4).Title("Force IPv4?").
			Description("pin the dialer to IPv4 (corporate-VPN / broken-IPv6 fix)").
			Value(&cur.ForceIPv4),
		huh.NewConfirm().Key(configFieldSave).Title("Save this config?").
			Affirmative("Save").Negative("Cancel").Value(save),
	}

	allFields := make([]huh.Field, 0, len(fields)+1)
	allFields = append(allFields,
		huh.NewNote().
			Title("secopsctl config").
			Description("Click a field or use ↑/↓/Tab; scroll for more. Click Save or Cancel when finished."),
	)
	allFields = append(allFields, fields...)

	keymap := huh.NewDefaultKeyMap()
	keymap.Input.Prev.SetKeys("up", "shift+tab")
	keymap.Input.Prev.SetHelp("↑/shift+tab", "back")
	keymap.Input.Next.SetKeys("down", "enter", "tab")
	keymap.Input.Next.SetHelp("↓/enter", "next")
	keymap.Confirm.Prev.SetKeys("up", "shift+tab")
	keymap.Confirm.Prev.SetHelp("↑/shift+tab", "back")
	keymap.Confirm.Next.SetKeys("down", "enter", "tab")
	keymap.Confirm.Next.SetHelp("↓/enter", "next")

	form := huh.NewForm(huh.NewGroup(allFields...)).WithKeyMap(keymap)
	return form, fields
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

// normalizeConfigValues applies the same cleanup to form and flag input before
// validation and persistence.
func normalizeConfigValues(i *config.Instance) {
	i.ProjectID = strings.TrimSpace(i.ProjectID)
	i.SetProjectNumber(strings.TrimSpace(i.ProjectNumberString()))
	i.Region = strings.TrimSpace(i.Region)
	i.CustomerID = strings.TrimSpace(i.CustomerID)
	i.SOARURL = normalizeSOARURL(strings.TrimSpace(i.SOARURL))
	i.SOARAppKey = strings.TrimSpace(i.SOARAppKey)
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
