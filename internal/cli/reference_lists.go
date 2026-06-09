package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// The `reference_lists` command holds one-off operations for no-delete lists
// that sit beside the pull/push reconcile loop.
func init() { rootCmd.AddCommand(newReferenceListsCmd()) }

func newReferenceListsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reference_lists <verb>",
		Short: "Reference-list operations beyond pull/push",
		Long: "Operate on reference lists outside the reconcile loop. Config-as-code is\n" +
			"`pull reference_lists` / `push reference_lists`; no-delete neutralization\n" +
			"commands are guarded and dry-run by default.",
	}
	cmd.AddCommand(newReferenceListsEmptyCmd())
	return cmd
}

func newReferenceListsEmptyCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "empty <name>",
		Short: "MUTATING (guarded): clear all entries from a reference list",
		Long: "Clear all entries from one reference list by name or full resource name.\n" +
			"The command resolves the list first and previews only metadata plus entry\n" +
			"count, not entry values. Guarded: dry-run by default, --yes to apply.\n" +
			"Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("reference list name is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			rl, err := c.GetReferenceList(ctx, name, true)
			if err != nil {
				return err
			}
			display := rl.DisplayName
			if display == "" {
				display = lastSegment(rl.Name)
			}
			if display == "" {
				display = name
			}
			id := lastSegment(rl.Name)
			action := fmt.Sprintf("reference_lists empty %q (%s): entries=%d -> 0",
				display, id, len(rl.Entries))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				_, err := c.UpdateReferenceList(ctx, rl.Name, "", []string{})
				return err
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
