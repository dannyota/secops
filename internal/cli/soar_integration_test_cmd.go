package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSOARIntegrationTestCmd() *cobra.Command {
	var instanceID string
	cmd := &cobra.Command{
		Use:   "test <identifier>",
		Short: "Test integration connectivity (default instance, or --instance <id>)",
		Long: "Run a connectivity test on the specified integration's instance.\n" +
			"By default tests the first configured instance; pass --instance <id>\n" +
			"to test a specific one. Reports pass/fail with the error message.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id := args[0]

			target := instanceID
			if target == "" {
				insts, ierr := listIntegrationInstances(ctx, lc, id)
				if ierr != nil {
					return fmt.Errorf("list instances for %q: %w", id, ierr)
				}
				if len(insts) == 0 {
					return fmt.Errorf("no instances found for integration %q", id)
				}
				target = insts[0].Identifier
			}

			raw, err := lc.TestStoreIntegration(ctx, target)
			if err != nil {
				return fmt.Errorf("test integration %q instance %q: %w", id, target, err)
			}

			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}

			var result struct {
				Successful bool   `json:"successful"`
				Message    string `json:"message"`
			}
			_ = json.Unmarshal(raw, &result)

			if result.Successful {
				fmt.Fprintf(cmd.OutOrStdout(), "PASS  %s (instance %s)\n", id, target)
			} else {
				msg := result.Message
				if msg == "" {
					msg = "unknown error"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "FAIL  %s (instance %s): %s\n", id, target, msg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&instanceID, "instance", "", "specific instance ID to test (default: first configured)")
	return markJSON(cmd)
}
