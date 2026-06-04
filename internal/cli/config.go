package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"danny.vn/secops/config"
)

func init() {
	var (
		fProjectID      string
		fProjectNumber  string
		fRegion         string
		fCustomerID     string
		fSOARURL        string
		fSOARAppKey     string
		fNonInteractive bool
	)

	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"init"},
		Short:   "Set up the secopsctl config (~/.secopsctl/instance.yaml)",
		Long: "Create or edit the instance config. Prompts for each value (pre-filled\n" +
			"with the current one; press Enter to keep it); the SOAR AppKey prompt is\n" +
			"hidden. Flags set values without prompting; --non-interactive (or piped\n" +
			"stdin) skips all prompts and writes flags + current values.\n\n" +
			"Writes ~/.secopsctl/instance.yaml (0600), or the --config path if given.\n" +
			"The file may hold the SOAR AppKey in plaintext (v1); it is git-ignored and\n" +
			"never committed. Resolution at run time: real SECOPS_* env vars override\n" +
			"the file. The mintable OAuth/ADC SIEM token is never stored — `gcloud auth\n" +
			"application-default login` handles SIEM auth.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			interactive := !fNonInteractive && term.IsTerminal(int(os.Stdin.Fd()))
			if interactive {
				r := bufio.NewReader(os.Stdin)
				cur.ProjectID = promptValue(r, "Project ID", cur.ProjectID)
				cur.SetProjectNumber(promptValue(r, "Project number", cur.ProjectNumberString()))
				cur.Region = promptValue(r, "Region (e.g. us, eu)", cur.Region)
				cur.CustomerID = promptValue(r, "Customer ID (GUID)", cur.CustomerID)
				cur.SOARURL = promptValue(r, "SOAR URL (optional, blank to skip)", cur.SOARURL)
				cur.SOARAppKey = promptSecret("SOAR AppKey", cur.SOARAppKey)
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
	f.StringVar(&fRegion, "region", "", "SecOps region (e.g. us, eu)")
	f.StringVar(&fCustomerID, "customer-id", "", "SecOps customer ID (GUID)")
	f.StringVar(&fSOARURL, "soar-url", "", "SOAR host URL (optional)")
	f.StringVar(&fSOARAppKey, "soar-app-key", "", "SOAR AppKey (optional; avoid on shared shells — prefer the prompt)")
	f.BoolVar(&fNonInteractive, "non-interactive", false, "do not prompt; write flags + current values")

	rootCmd.AddCommand(cmd)
}

// requiredMissing lists the required identifiers still empty after flags/prompts.
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

// promptValue prints "Label [current]: " and returns the typed line, or current
// when the user presses Enter. Reads from r (a buffered stdin).
func promptValue(r *bufio.Reader, label, current string) string {
	if current != "" {
		fmt.Printf("%s [%s]: ", label, current)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return current
	}
	return strings.TrimSpace(line)
}

// promptSecret reads the AppKey without echoing. It shows whether one is already
// set and keeps the existing value when the user enters nothing.
func promptSecret(label, current string) string {
	state := "unset"
	if current != "" {
		state = "set"
	}
	fmt.Fprintf(os.Stderr, "%s [%s] (hidden, Enter to keep): ", label, state)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return current
	}
	entered := strings.TrimSpace(string(b))
	if entered == "" {
		return current
	}
	return entered
}
