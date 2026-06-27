package cli

import "github.com/spf13/cobra"

// newListsCmd is the top-level `lists` group (the console's "Lists"): reference
// lists and watchlists. `lists empty` neutralizes a reference list (config-as-code
// for reference lists is `pull`/`push reference_lists` — the snake_case target is
// unchanged); `lists watchlists …` manages curated entity watchlists.
func init() { rootCmd.AddCommand(newListsCmd()) }

func newListsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lists <area>",
		Short: "Lists: reference lists (empty) and watchlists",
		Long: "Lookup-data lists:\n" +
			"  empty          neutralize a reference list (config-as-code: pull/push reference_lists)\n" +
			"  watchlists …   manage curated entity watchlists",
	}
	cmd.AddCommand(newReferenceListsEmptyCmd(), newWatchlistsCmd())
	return cmd
}
