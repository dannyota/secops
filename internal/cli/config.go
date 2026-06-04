package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"danny.vn/secops/internal/userdir"
)

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the user-level config in ~/.secopsctl",
		Long: "Manage the user-level secopsctl config directory (~/.secopsctl, or\n" +
			"$SECOPSCTL_HOME). secopsctl loads ~/.secopsctl/.env on startup, so a\n" +
			"long-lived secret stored there is picked up automatically by every command.",
	}

	setSOARKeyCmd := &cobra.Command{
		Use:   "set-soar-key",
		Short: "Store the SOAR AppKey in ~/.secopsctl/.env",
		Long: "Prompt for the SOAR AppKey and write it to ~/.secopsctl/.env (0600) as\n" +
			"SECOPS_SOAR_APP_KEY, so `secopsctl soar ...` picks it up automatically.\n" +
			"The key is never echoed. When stdin is not a terminal the key is read\n" +
			"from stdin (one line), e.g. `printf %s \"$KEY\" | secopsctl config set-soar-key`.\n\n" +
			"Generate the key in the Chronicle SOAR UI:\n" +
			"  Settings -> Advanced -> API Keys -> Add  (long-lived, admin-scoped).",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			key, err := readSecret("Enter SOAR AppKey (input hidden): ")
			if err != nil {
				return err
			}
			if key = strings.TrimSpace(key); key == "" {
				return fmt.Errorf("no key provided")
			}
			path, err := userdir.SetEnvVar("SECOPS_SOAR_APP_KEY", key)
			if err != nil {
				return err
			}
			fmt.Printf("Wrote SECOPS_SOAR_APP_KEY to %s (%d chars). Loaded automatically on next run.\n", path, len(key))
			return nil
		},
	}

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print user-level config paths and whether each exists",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			for _, p := range []string{userdir.Dir(), userdir.InstanceConfigPath(), userdir.EnvFilePath()} {
				status := "missing"
				if _, err := os.Stat(p); err == nil {
					status = "present"
				}
				fmt.Printf("%-8s %s\n", status, p)
			}
		},
	}

	configCmd.AddCommand(setSOARKeyCmd, pathCmd)
	rootCmd.AddCommand(configCmd)
}

// readSecret reads a secret without echoing it when stdin is a terminal;
// otherwise (piped/non-interactive) it reads a single line from stdin. The
// prompt is written to stderr so stdout stays clean for redirection.
func readSecret(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return sc.Text(), nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no input on stdin")
}
