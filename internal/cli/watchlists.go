package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newWatchlistsCmd is the read-only watchlists surface (SIEM plane, v1). A
// watchlist groups entities an analyst tracks; this lists/gets them.
func newWatchlistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchlists",
		Short: "Read-only: list/get SIEM entity watchlists",
	}
	cmd.AddCommand(newWatchlistsListCmd(), newWatchlistsGetCmd())
	return cmd
}

func newWatchlistsListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List watchlists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			wls, err := c.ListWatchlists(baseContext(), limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(wls)
			}
			for _, w := range wls {
				fmt.Fprintf(os.Stdout, "%-28s %s\n", w.WatchlistID(), w.DisplayName)
			}
			fmt.Fprintf(os.Stdout, "\n%d watchlist(s)\n", len(wls))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "max watchlists to fetch/show")
	return cmd
}

func newWatchlistsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one watchlist by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			w, err := c.GetWatchlist(baseContext(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(w)
		},
	}
	return cmd
}

func init() { rootCmd.AddCommand(newWatchlistsCmd()) }
