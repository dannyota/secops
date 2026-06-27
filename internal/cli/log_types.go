package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `log-types` command lists the log types active/known on the instance and
// describes one — the catalog a platform engineer onboards sources against.
// Read-only.

func newLogTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log-types <verb>",
		Short: "List the instance's log types and describe one (read-only)",
	}
	cmd.AddCommand(newLogTypesListCmd(), newLogTypesGetCmd())
	return cmd
}

func newLogTypesListCmd() *cobra.Command {
	var search string
	var limit int
	cmd := &cobra.Command{
		Use:   "list [--search <pattern>] [--limit N]",
		Short: "Read-only: list log types (id + display name), optionally filtered",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			lts, err := c.ListLogTypes(baseContext(), search, limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(lts)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "LOG TYPE\tDISPLAY NAME")
			for _, lt := range lts {
				fmt.Fprintf(tw, "%s\t%s\n", lastSegment(lt.Name), lt.DisplayName)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d log type(s).\n", len(lts))
			return nil
		},
	}
	cmd.Flags().StringVar(&search, "search", "", "case-insensitive substring filter on id/display name (applied to the scanned set)")
	cmd.Flags().IntVar(&limit, "limit", 5000, "max log types to scan (the catalog is ~thousands; raise to widen a --search)")
	return markJSON(cmd)
}

func newLogTypesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <log-type>",
		Short: "Read-only: the description of one log type",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			desc, err := c.GetLogTypeDescription(baseContext(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, desc)
			return nil
		},
	}
	return cmd
}
