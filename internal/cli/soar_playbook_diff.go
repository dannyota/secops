package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSOARPlaybookDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <name|identifier> <local-file>",
		Short: "Compare the live playbook definition with a local export",
		Long: "Fetch the live playbook and diff it against a local JSON file (e.g. from\n" +
			"`export`). Shows a unified diff of the canonicalized JSON. Useful for\n" +
			"reviewing changes before `deploy` or checking for drift.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier := args[0]
			if !looksLikeUUID(identifier) {
				identifier, err = resolvePlaybookDefinition(ctx, lc, args[0])
				if err != nil {
					return err
				}
			}

			liveRaw, err := lc.GetPlaybook(ctx, identifier)
			if err != nil {
				return err
			}

			localBytes, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}

			liveCanon, err := canonicalJSON(liveRaw)
			if err != nil {
				return fmt.Errorf("canonicalize live: %w", err)
			}
			localCanon, err := canonicalJSON(json.RawMessage(localBytes))
			if err != nil {
				return fmt.Errorf("canonicalize local: %w", err)
			}

			if liveCanon == localCanon {
				fmt.Fprintln(cmd.OutOrStdout(), "No differences.")
				return nil
			}

			diff := unifiedDiff(liveCanon, localCanon, "live:"+args[0], "local:"+args[1])
			fmt.Fprint(cmd.OutOrStdout(), diff)
			return nil
		},
	}
	return cmd
}

func canonicalJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
